package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/agent"
	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/excerpt"
	"github.com/miraassist/mira/internal/inbox"
	"github.com/miraassist/mira/internal/mailcal"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// Mira's inbox (docs/spec/inbox.md): Paperless's webhook brings a document
// with the tag "Mira"; Mira reads it through the skill paperless's MCP
// server, asks its owner, and marks it read with "Mira gelesen".

// inboxSkill is the skill whose MCP server and inbox_token the inbox uses.
const inboxSkill = "paperless"

// inboxToken is the skill paperless's inbox_token; "" when the inbox is off.
func inboxToken(cfg config.Config) string {
	v, _ := cfg.Skills.Enabled[inboxSkill]["inbox_token"].(string)
	return strings.TrimSpace(v)
}

// newInbox makes the inbox when the skill postfach is loaded (it is always
// on); nil otherwise, with a log line. Its Paperless part needs the skill
// paperless with its server and inbox_token; its mail part only the IMAP
// device "Postfach …" in Home Assistant.
func (a *app) newInbox(cfg config.Config, loaded []skills.Skill) *inbox.Service {
	var postfach, paperless *skills.Skill
	for i := range loaded {
		switch loaded[i].Name {
		case inbox.Target:
			postfach = &loaded[i]
		case inboxSkill:
			paperless = &loaded[i]
		}
	}
	if postfach == nil || postfach.Inbox == nil {
		a.log.Warn("the inbox is off: the skill postfach is not loaded")
		return nil
	}
	o := inbox.Options{Model: a.inboxRead(),
		Extract: postfach.Inbox.Extract, Person: a.inboxPerson, Calendar: a.inboxCalendar,
		Ask: a.inboxAsk, Tell: a.inboxTell, House: store.ReceiverHouse,
		Learn: a.inboxLearn, Owner: a.inboxOwner,
		Open: func(ctx context.Context, key string) bool {
			return a.threads != nil && a.threads.OpenWith(ctx, inbox.Target, inbox.FieldDoc, key)
		},
		Known: a.inboxKnown, Keep: a.inboxKeep,
		Lang: a.lang, Logger: a.log,
	}
	token := inboxToken(cfg)
	switch {
	case token == "":
		a.log.Info("the inbox has no Paperless part: the skill paperless has no inbox_token")
	case paperless == nil || paperless.MCP == nil:
		a.log.Warn("the inbox has no Paperless part: the skill paperless with its server is not loaded")
	default:
		o.Token, o.Paperless = token, paperlessMCP{a: a, server: paperless.MCP.Name}
		a.log.Info("the inbox takes Paperless's webhooks", "path", inbox.Path)
	}
	a.log.Info("the inbox is on", "mail_device", "Postfach <login>")
	return inbox.New(o)
}

// kvInboxMails keeps the hashes of the mails the inbox has read, the last
// mailsKept per IMAP entry, oldest first (docs/spec/inbox.md, decision 24).
const kvInboxMails = "inbox_mails"

const mailsKept = 100

func (a *app) inboxMails(ctx context.Context) (map[string][]string, error) {
	out := map[string][]string{}
	raw, err := a.store.GetKV(ctx, kvInboxMails)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("the mails read %q: %w", excerpt.Of(raw), err)
	}
	return out, nil
}

func (a *app) inboxKnown(ctx context.Context, entry, hash string) (bool, error) {
	mails, err := a.inboxMails(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(mails[entry], hash), nil
}

func (a *app) inboxKeep(ctx context.Context, entry, hash string) error {
	mails, err := a.inboxMails(ctx)
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(mails[entry], func(h string) bool { return h == hash })
	kept = append(kept, hash)
	if len(kept) > mailsKept {
		kept = kept[len(kept)-mailsKept:]
	}
	mails[entry] = kept
	raw, _ := json.Marshal(mails)
	return a.store.SetKV(ctx, kvInboxMails, string(raw))
}

// inboxTools is inbox_enter, wired to the threads and the calendars.
func (a *app) inboxTools() []tools.InternalTool {
	if a.inbox == nil {
		return nil
	}
	return a.inbox.Tools(inbox.ToolOptions{
		Thread: func(ctx context.Context, inv tools.Invocation) (inbox.Thread, bool) {
			th, ok := a.threads.TurnThreadOf(ctx, inv, inbox.Target)
			if !ok {
				return inbox.Thread{}, false
			}
			return inbox.Thread{ID: th.ID, Person: th.Person, Fields: th.Fields}, true
		},
		Calendars: a.inboxCalendars,
		Create: func(ctx context.Context, entity string, e inbox.Entry) error {
			return mailCalendar{a}.Create(ctx, entity, mailcal.Entry{Summary: e.Summary, Description: e.Description,
				Location: e.Location, Day: e.Day, Start: e.Start, End: e.End})
		},
		Tick: func(ctx context.Context, inv tools.Invocation, step int, note string) error {
			_, err := a.threads.PlanNote(ctx, inv, "check", "", step, note)
			return err
		},
	})
}

func (a *app) inboxRead() isolatedAgent {
	return isolatedAgent{a: a, name: "inbox.read", origin: agent.FromCode, limits: mailLimits}
}

// inboxPerson maps a Paperless user name to a person: the login of their
// Home Assistant user, their name, or their key, case aside (the convention
// of docs/spec/calendar.md).
func (a *app) inboxPerson(owner string) (string, bool) {
	return personByOwner(owner, a.persons(), a.logins(context.Background()))
}

// personByOwner is the person whose login (logins: person entity to login),
// name or key is owner, case aside.
func personByOwner(owner string, persons map[string]config.Person, logins map[string]string) (string, bool) {
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	want := norm(owner)
	if want == "" {
		return "", false
	}
	for key, p := range persons {
		if norm(logins[p.Entity]) == want || norm(p.Name) == want || norm(key) == want {
			return key, true
		}
	}
	return "", false
}

// inboxCalendars are the calendars with whose they are by convention.
func (a *app) inboxCalendars(ctx context.Context) ([]inbox.Calendar, error) {
	cals, err := a.calendarList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]inbox.Calendar, 0, len(cals))
	for _, c := range cals {
		out = append(out, inbox.Calendar{Entity: c.Entity, Name: c.Name, Writable: c.Writable, Of: a.calendarOwner(ctx, c.Name)})
	}
	return out, nil
}

// calendarOwner is the person whose calendar this name is by convention
// (docs/spec/calendar.md, Personal calendars): named like their login or
// their person, case and spaces aside; "" for none.
func (a *app) calendarOwner(ctx context.Context, name string) string {
	key, _ := personByOwner(name, a.persons(), a.logins(ctx))
	return key
}

// inboxCalendar is the person's own writable calendar by convention.
func (a *app) inboxCalendar(ctx context.Context, person string) (string, error) {
	cals, err := a.inboxCalendars(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range cals {
		if c.Of == person && c.Writable {
			return c.Entity, nil
		}
	}
	return "", nil
}

// inboxAsk starts the person's inbox thread with the findings and leaves its
// question as a message of the thread: delivered by the person's settings,
// withdrawn when the thread ends.
func (a *app) inboxAsk(ctx context.Context, q inbox.Question) error {
	if a.threads == nil {
		return errors.New("no threads")
	}
	raw, err := json.Marshal(q.Findings)
	if err != nil {
		return err
	}
	th, err := a.threads.StartFor(ctx, inbox.Target, q.Person, q.Summary, q.Plan, map[string]string{
		inbox.FieldDoc: q.Doc.Key, inbox.FieldTitle: q.Doc.Title, inbox.FieldRef: q.Doc.Ref, inbox.FieldFindings: string(raw)})
	if err != nil {
		return err
	}
	if err := a.threads.Note(ctx, th.ID, q.Text); err != nil {
		a.log.Warn("could not note the inbox question in its thread", "thread", th.ID, "error", err)
	}
	_, _, err = a.store.PutMessage(ctx, store.Message{ID: ulid.Make().String(), Kind: store.KindPostfach, Source: store.SourceTask,
		DedupeKey: "inbox:" + th.ID, Importance: 1, Receiver: q.Person, Spoken: q.Text, ThreadID: th.ID,
		ExpiresAt: th.ExpiresAt})
	return err
}

// inboxTell leaves a message of the inbox for a person or the house.
func (a *app) inboxTell(ctx context.Context, receiver, text string) error {
	_, _, err := a.store.PutMessage(ctx, store.Message{ID: ulid.Make().String(), Kind: store.KindSimple, Source: store.SourceTask,
		DedupeKey: "inbox:" + ulid.Make().String(), Importance: 1, Receiver: receiver, Spoken: text,
		ExpiresAt: time.Now().Add(72 * time.Hour)})
	return err
}

// paperlessMCP is Paperless through the skill paperless's MCP server, called
// by Mira's code (mcp.Host.CallServer).
type paperlessMCP struct {
	a      *app
	server string
}

func (p paperlessMCP) call(ctx context.Context, tool string, args any, into any) error {
	raw, _ := json.Marshal(args)
	res, err := p.a.mcp.CallServer(ctx, p.server, tool, raw)
	if err != nil {
		return err // a lost connection or no server: retryable
	}
	if res.IsError {
		return toolError(tool, res.Text)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal([]byte(res.Text), into); err != nil {
		return retry.Fatal(fmt.Errorf("%s: unreadable answer %q: %w", tool, excerpt.Of(res.Text), err))
	}
	return nil
}

// toolError classifies the MCP server's error answer by the HTTP status it
// names: 404 is a document gone, another 4xx a refusal (fatal), the rest
// (5xx, none) retryable.
func toolError(tool, text string) error {
	var e struct {
		Error  string `json:"error"`
		Status int    `json:"status"`
	}
	_ = json.Unmarshal([]byte(text), &e)
	err := fmt.Errorf("%s: %s", tool, excerpt.Of(text))
	switch {
	case e.Status == 404:
		return &inbox.Gone{Err: err}
	case e.Status == 429:
		return retry.Transient(err)
	case e.Status >= 400 && e.Status < 500:
		return retry.Fatal(err)
	}
	return retry.Transient(err)
}

func (p paperlessMCP) Doc(ctx context.Context, id int) (inbox.Doc, error) {
	var d struct {
		Title   string `json:"title"`
		Created string `json:"created"`
		Owner   int    `json:"owner"`
	}
	if err := p.call(ctx, "get_document", map[string]any{"id": id}, &d); err != nil {
		return inbox.Doc{}, err
	}
	created, _ := time.ParseInLocation("2006-01-02", strings.TrimSpace(d.Created), time.Local)
	return inbox.Doc{ID: id, Title: d.Title, Created: created, Owner: d.Owner}, nil
}

func (p paperlessMCP) Text(ctx context.Context, id int) (string, error) {
	var d struct {
		Content string `json:"content"`
	}
	err := p.call(ctx, "get_document_content", map[string]any{"id": id}, &d)
	return d.Content, err
}

func (p paperlessMCP) Tag(ctx context.Context, name string) (int, bool, error) {
	var d struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := p.call(ctx, "list_tags", map[string]any{"name__iexact": name}, &d); err != nil {
		return 0, false, err
	}
	for _, t := range d.Results {
		if strings.EqualFold(t.Name, name) {
			return t.ID, true, nil
		}
	}
	return 0, false, nil
}

func (p paperlessMCP) AddTag(ctx context.Context, doc, tag int) error {
	return p.call(ctx, "bulk_edit_documents", map[string]any{"documents": []int{doc}, "method": "add_tag", "tag": tag}, nil)
}

// Unread pages through query_documents: the documents with the tag inbox and
// without the tag read, 25 per page.
func (p paperlessMCP) Unread(ctx context.Context, tagInbox, tagRead int) ([]int, error) {
	var out []int
	for page := 1; ; page++ {
		var d struct {
			Next    *string `json:"next"`
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		args := map[string]any{"page": page, "page_size": 25, "paperless_filters": map[string]any{
			"tags__id__all": strconv.Itoa(tagInbox), "tags__id__none": strconv.Itoa(tagRead)}}
		if err := p.call(ctx, "query_documents", args, &d); err != nil {
			return nil, err
		}
		for _, r := range d.Results {
			out = append(out, r.ID)
		}
		if d.Next == nil || len(d.Results) == 0 || page >= 40 {
			return out, nil
		}
	}
}

// kvInboxOwners keeps the owners learned from the webhooks: Paperless's owner
// number to user name (docs/spec/inbox.md, decision 18).
const kvInboxOwners = "inbox_owners"

func (a *app) inboxOwners(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	raw, err := a.store.GetKV(ctx, kvInboxOwners)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("the learned owners %q: %w", excerpt.Of(raw), err)
	}
	return out, nil
}

func (a *app) inboxLearn(ctx context.Context, owner int, name string) error {
	owners, err := a.inboxOwners(ctx)
	if err != nil {
		return err
	}
	key := strconv.Itoa(owner)
	if owners[key] == name {
		return nil
	}
	owners[key] = name
	raw, _ := json.Marshal(owners)
	return a.store.SetKV(ctx, kvInboxOwners, string(raw))
}

func (a *app) inboxOwner(ctx context.Context, owner int) (string, bool, error) {
	owners, err := a.inboxOwners(ctx)
	if err != nil {
		return "", false, err
	}
	name, ok := owners[strconv.Itoa(owner)]
	return name, ok, nil
}

// inboxCatchUp catches the inbox up once its MCP server is connected: at
// once when it is, else when the MCP host's tools change (a server came).
// The inbox catches up once per start.
func (a *app) inboxCatchUp(ctx context.Context) error {
	if !a.inbox.PaperlessOn() {
		return nil
	}
	for {
		for _, st := range a.mcp.Status() {
			if st.Name == inboxSkill && st.Connected {
				a.inbox.CatchUp(ctx)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-a.toolsChanged:
		}
	}
}
