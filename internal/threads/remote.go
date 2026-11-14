package threads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/store"
	"github.com/miraassist/mira/internal/tools"
)

// Agent requests (docs/spec/remote-agents.md, D): a remote agent filling a
// field asks back; its question goes to the thread's owner as a message of
// the kind agent_request, and the owner's answer goes back to it with the
// tool agent_answer.

// ToolAgentAnswer passes the owner's answer on to a remote agent.
const ToolAgentAnswer = "agent_answer"

// agentRequestKey keys a call's agent request: a second question of the
// same call replaces the first while that is still open (decision 21).
func agentRequestKey(callID string) string { return "agent:" + callID }

// AgentRequest takes a remote agent's question into its thread: a line of
// the log, and the question to the owner. A turn still waiting for the
// thread's agents gets it in its answer; else it comes as a message of the
// kind agent_request. An error when the thread no longer waits for that
// call, or the message cannot be put.
func (m *Manager) AgentRequest(ctx context.Context, id, into, callID, label, question string) error {
	t := m.texts()
	say := fmt.Sprintf(t.agentAsks, label, strings.TrimSpace(question))
	th, err := m.change(ctx, id, func(th *store.Thread) error {
		if !store.ThreadOpen(th.State) || th.Calls[into] != callID {
			return errStale
		}
		m.logEntry(th, "mira", say)
		m.extend(th)
		return nil
	})
	if errors.Is(err, errStale) {
		return fmt.Errorf("the thread %s no longer waits for the call %s", id, callID)
	}
	if err != nil {
		return err
	}
	m.log.Info("agent request in thread", "thread", id, "call_id", callID, "agent", label)
	if m.handOver(id, say) {
		// Said in the turn that waits for it (decision 41): no message.
		return nil
	}
	if m.opts.Deliver == nil {
		return errors.New("no way to deliver messages")
	}
	receiver, area := m.owner(th)
	n := store.Message{ID: ulid.Make().String(), Kind: store.KindAgentRequest, Source: store.SourceTask,
		DedupeKey: agentRequestKey(callID), Importance: 2, Receiver: receiver, Spoken: say, Details: m.summary(th),
		ThreadID: th.ID, ExpiresAt: th.ExpiresAt, OriginDevice: th.Device}
	if err := m.opts.Deliver(ctx, n, area); err != nil {
		return fmt.Errorf("could not deliver the agent request: %w", err)
	}
	return nil
}

// owner is whom a thread's messages go to, and the room they are said in:
// where the thread began, even when it belongs to a person.
func (m *Manager) owner(th store.Thread) (receiver, area string) {
	area = th.Area
	receiver = store.ReceiverHouse
	if a, ok := strings.CutPrefix(th.Place, "area:"); ok {
		area = a
	} else if p, ok := strings.CutPrefix(th.Place, "person:"); ok {
		receiver = p
	} else if th.Place != store.PlaceHouse {
		receiver = m.chatReceiver(th.Place)
	}
	return receiver, area
}

// chatReceiver is whom a thread of a chat belongs to: the chat's person,
// else the house.
func (m *Manager) chatReceiver(place string) string {
	var persons map[string]config.Person
	if m.opts.Persons != nil {
		persons = m.opts.Persons()
	}
	return tools.ReceiverFor("", tools.Invocation{SessionKey: place, Channel: config.ChannelTelegram}, persons)
}

// asking is the call of th that waits for an answer to its agent request.
func (m *Manager) asking(th store.Thread) (into, callID, label, question string, ok bool) {
	if m.opts.Asking == nil {
		return "", "", "", "", false
	}
	for _, f := range sortedKeys(th.Calls) {
		if label, q, ok := m.opts.Asking(th.Calls[f]); ok {
			return f, th.Calls[f], label, q, true
		}
	}
	return "", "", "", "", false
}

// AgentAnswer passes the owner's answer on to the remote agent that asks in
// the turn's thread (or the thread named): sent, its agent request is done;
// not sent, it stays and the owner hears why (D.4).
func (m *Manager) AgentAnswer(ctx context.Context, inv tools.Invocation, id, text string) (string, error) {
	t := m.texts()
	var th store.Thread
	var err error
	if id == "" {
		th, err = m.current(ctx, inv)
	} else {
		th, err = m.opts.Store.GetThread(ctx, id)
		places := append(m.turnPlaces(ctx, inv), store.PlaceHouse)
		if err == nil && (!store.ThreadOpen(th.State) || !contains(places, th.Place)) {
			err = store.ErrNotFound
		}
	}
	if err != nil {
		return "", errors.New(t.noThread)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New(t.answerEmpty)
	}
	_, callID, label, question, ok := m.asking(th)
	if !ok {
		return "", errors.New(t.nobodyAsks)
	}
	if err := m.opts.Answer(ctx, callID, text); err != nil {
		m.log.Warn("the answer to an agent request could not be passed on; the request stays", "thread", th.ID,
			"call_id", callID, "agent", label, "error", err)
		return "", fmt.Errorf(t.answerNotSent, err)
	}
	if _, err := m.change(ctx, th.ID, func(now *store.Thread) error {
		m.logEntry(now, "user", text)
		m.logEntry(now, "mira", fmt.Sprintf(t.answerPassed, label))
		return nil
	}); err != nil {
		m.log.Warn("could not note the answer in the thread", "thread", th.ID, "error", err)
	}
	m.ackRequest(ctx, callID)
	m.log.Info("agent request answered", "thread", th.ID, "call_id", callID, "agent", label, "question", question)
	return fmt.Sprintf(t.answerPassed, label), nil
}

// ackRequest marks a call's agent request done, once answered.
func (m *Manager) ackRequest(ctx context.Context, callID string) {
	key := agentRequestKey(callID)
	msg, err := m.opts.Store.OpenMessageByDedupeKey(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		m.log.Warn("could not find the answered agent request; it stays until its thread ends", "call_id", callID, "error", err)
		return
	}
	for _, e := range msg.Entries {
		if e.DedupeKey != key {
			continue
		}
		if err := m.opts.Store.AckEntry(ctx, e.ID); err != nil {
			m.log.Warn("could not mark the answered agent request done; it stays until its thread ends", "call_id", callID,
				"entry", e.ID, "error", err)
		}
	}
}

// agentAnswerTool is the tool agent_answer.
func (m *Manager) agentAnswerTool() tools.InternalTool {
	t := m.texts()
	return tools.InternalTool{
		Name: ToolAgentAnswer, Description: t.answerDesc, AskAfterWeb: true,
		Schema: fmt.Sprintf(`{"type":"object","properties":{"thread":{"type":"string","description":%q},"text":{"type":"string","description":%q}},"required":["text"]}`,
			t.answerThreadDesc, t.answerTextDesc),
		Run: func(ctx context.Context, inv tools.Invocation) (string, error) {
			var a struct {
				Thread string `json:"thread"`
				Text   string `json:"text"`
			}
			if err := json.Unmarshal(inv.Args, &a); err != nil {
				return "", err
			}
			return m.AgentAnswer(ctx, inv, strings.TrimSpace(a.Thread), a.Text)
		},
	}
}
