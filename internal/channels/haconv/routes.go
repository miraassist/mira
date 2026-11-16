package haconv

import (
	"net/http"

	"github.com/miraassist/mira/internal/api"
	"github.com/miraassist/mira/internal/backup"
	"github.com/miraassist/mira/internal/inbox"
	"github.com/miraassist/mira/internal/mailcal"
	"github.com/miraassist/mira/internal/oauth"
	"github.com/miraassist/mira/internal/tasks"
)

// routes registers the integration's interface (docs/spec/openapi.md).
func (s *Server) routes() {
	m := s.mux
	const tag = api.TagIntegration
	m.Handle(api.Route{Method: http.MethodGet, Path: "/v1/health", Tag: tag,
		Summary: "The service's state, version and id, and its dependencies' checks", Response: Health{}}, s.health)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/conversation", Tag: tag,
		Summary:     "A turn: what the satellite or the chat heard, Mira's answer",
		Description: "With stream true (voice), the answer is application/x-ndjson: one line per piece and per tool call, then the whole answer (docs/spec/streaming.md).",
		Request:     conversationRequest{}, Response: conversationResponse{}, Errors: []int{400, 502}}, s.conversation)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/conversation/reset", Tag: tag,
		Summary: "Forget a conversation's history", Request: ConversationReset{}, Response: ResetDone{}, Errors: []int{400, 500}}, s.resetConversation)
	m.Handle(api.Route{Method: http.MethodGet, Path: "/v1/status", Tag: tag,
		Summary: "What waits, quiet mode, problems for the repairs", Response: Status{}, Errors: []int{500}}, s.status)
	m.Handle(api.Route{Method: http.MethodPut, Path: "/v1/quiet_mode", Tag: tag,
		Summary: "Turn quiet mode on or off", Request: QuietModeRequest{}, Response: QuietMode{}, Errors: []int{400, 500}}, s.quietMode)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/messages", Tag: tag,
		Summary:     "Leave a message for a person or the house",
		Description: "201 when the message was made, 200 when it was there already.",
		Request:     messageRequest{}, Response: MessageMade{}, Errors: []int{400, 500}}, s.addMessage)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/assess", Tag: tag,
		Summary: "mira.assess: judge a situation for an automation, maybe notify", Request: tasks.Request{}, Response: tasks.Verdict{},
		Errors: []int{400, 503}}, s.assess)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/decide", Tag: tag,
		Summary: "mira.decide: a yes or no for an automation", Request: tasks.Request{}, Response: tasks.Decision{},
		Errors: []int{400, 503}}, s.decide)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/check", Tag: tag,
		Summary: "mira.check: look at a group of entities", Request: tasks.CheckCall{}, Response: tasks.CheckAnswer{},
		Errors: []int{400, 502, 503}}, s.check)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/mail", Tag: tag,
		Summary:     "A mail of an IMAP device, for the mail calendar or the inbox",
		Description: "202 with {queued} when the mail is the inbox's.",
		Request:     mailcal.Mail{}, Response: mailcal.Outcome{}, Errors: []int{400, 404, 422, 500}}, s.mail)
	agents := s.opts.Agents != nil
	m.HandleIf(agents, api.Route{Method: http.MethodPost, Path: "/v1/agent", Tag: tag, Only: "with agents",
		Summary: "mira.agent: run one of Mira's agents", Request: agentRequest{}, Response: agentResponse{},
		Errors: []int{400, 404}}, s.callAgent)
	m.HandleIf(agents, api.Route{Method: http.MethodPost, Path: "/v1/agent/result", Tag: tag, Only: "with agents",
		Summary: "The result of an agent's call that ran on", Request: AgentCall{}, Response: agentResponse{},
		Errors: []int{400, 404, 500}}, s.agentResult)
	m.HandleIf(agents, api.Route{Method: http.MethodPost, Path: "/v1/agent/cancel", Tag: tag, Only: "with agents",
		Summary: "Cancel an agent's call", Request: AgentCall{}, Response: AgentCancelled{}, Errors: []int{400}}, s.cancelAgent)
	m.HandleIf(s.opts.StartSkill != nil, api.Route{Method: http.MethodPost, Path: "/v1/agent/skill", Tag: api.TagSkills,
		Only:    "with skills that start from Home Assistant",
		Summary: "Start a skill's thread (mira.agent with skill)", Request: SkillStart{}, Response: SkillStarted{},
		Errors: []int{400, 500}}, s.startSkill)
	m.HandleIf(s.opts.Eval, api.Route{Method: http.MethodPost, Path: "/v1/eval/reset", Tag: tag, Only: "in eval mode (docs/spec/evals.md), for tests",
		Summary: "Empty Mira's state between eval scenarios", Request: EvalReset{}, Response: Done{}, Errors: []int{400, 500}}, s.evalReset)
	logins := s.opts.Logins != nil
	m.HandleIf(logins, api.Route{Method: http.MethodGet, Path: "/v1/oauth/{skill}", Tag: api.TagSkills, Only: "with skills' connections",
		Summary: "What the integration starts a skill's login with", Response: oauth.Login{}, Errors: []int{404, 422}}, s.oauthLogin)
	m.HandleIf(logins, api.Route{Method: http.MethodPut, Path: "/v1/oauth/{skill}/token", Tag: api.TagSkills, Only: "with skills' connections",
		Summary: "Hand over a skill's login", Request: oauth.Handover{}, Response: Connected{}, Errors: []int{400, 404, 422}}, s.oauthToken)
	withBackup := s.opts.Backup != nil
	m.HandleIf(withBackup, api.Route{Method: http.MethodGet, Path: "/v1/backup", Tag: tag, Only: "with backups",
		Summary: "Mira's snapshot for Home Assistant's backup (not with a partner token)", Response: "", ResponseMedia: "application/gzip",
		Errors: []int{403, 500}}, s.backup)
	m.HandleIf(withBackup, api.Route{Method: http.MethodPost, Path: "/v1/restore", Tag: tag, Only: "with backups",
		Summary: "Put a snapshot in place; the service starts anew (not with a partner token)", Request: "", RequestMedia: "application/gzip",
		Response: backup.Result{}, Errors: []int{400, 403, 500}}, s.restore)
	m.Before(api.Route{Method: http.MethodPost, Path: InboxPath, Tag: tag, Open: true,
		Summary:     "Paperless's webhook for the inbox (docs/spec/inbox.md)",
		Description: "Takes the inbox's own token (the skill paperless's inbox_token) as bearer, not the service's.",
		Request:     inbox.Webhook{}, Response: inbox.Queued{}, Status: http.StatusAccepted, Errors: []int{400, 401, 404, 405}})

	// Home Assistant through Mira's interface (docs/spec/ha-api.md).
	m.Handle(api.Route{Method: http.MethodGet, Path: "/v1/ha/states/{entity_id}", Tag: api.TagHA,
		Summary:     "One entity's state as Home Assistant gives it",
		Description: "Home Assistant's GET /api/states/<entity_id>, with Mira's token, passed on unchanged. One request, no retry.",
		Response:    HAState{}, Errors: []int{400, 404, 502, 503}}, s.haState)
	m.Handle(api.Route{Method: http.MethodPost, Path: "/v1/ha/services/{domain}/{service}", Tag: api.TagHA,
		Summary: "Call a Home Assistant action with Mira's connection",
		Description: "The body is the action's data as Home Assistant takes it, the target inside it (entity_id, area_id, …): " +
			"each action takes its own fields, so it is an open object, passed on unchanged. " +
			"With return_response, the answer holds the action's response as it came. Tried once; no hooks.",
		Query:   []api.Param{{Name: "return_response", Description: "Present: ask for the action's response."}},
		Request: HAActionData{}, Response: HAActionResult{}, Errors: []int{400, 502, 503}}, s.haService)
}
