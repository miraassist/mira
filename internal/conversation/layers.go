package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
	"github.com/miraassist/mira/internal/llm"
	"github.com/miraassist/mira/internal/prompt"
	"github.com/miraassist/mira/internal/store"
)

// Prefix assembles layers 1 to 5 for a channel. Every channel gets the same
// tools, so the start of the prompt is shared; that background jobs only
// read is enforced by the guard, not by hiding tools.
func (e *Engine) Prefix(ctx context.Context, ch config.Channel) (prompt.Prefix, error) {
	// Skills' texts follow the turn's language (docs/spec/skill-sprachen.md).
	defs := e.tools.Defs(e.lang(ctx), false)
	toolList, err := prompt.BuildTools(defs)
	if err != nil {
		return prompt.Prefix{}, err
	}
	profile, _, err := e.store.Profile(ctx, store.OwnerHouse)
	if err != nil {
		return prompt.Prefix{}, err
	}
	return prompt.Prefix{
		Lang:      e.lang(ctx),
		Agent:     prompt.AgentLayer(e.lang(ctx), e.currentPersona()),
		Style:     prompt.StyleFor(e.lang(ctx), ch),
		Tools:     toolList,
		Structure: e.structureText(),
		Profile:   clipProfile(profile, e.cfg.Memory.MaxProfileChars),
		Examples:  prompt.Examples(e.lang(ctx), toolList),
	}, nil
}

// currentPersona is the persona as the web UI last set it, else the
// config's.
func (e *Engine) currentPersona() string {
	if e.persona != nil {
		return e.persona()
	}
	return e.cfg.Prompt.Persona
}

// structureText is layer 4: Home Assistant's Assist prompt when there is
// one, otherwise the agent's own block built from the registries.
func (e *Engine) structureText() string {
	if e.assist != nil {
		if t := e.assist(); t != "" {
			return t
		}
	}
	return e.structure().Block
}

// deviceHintsFor are the lines of Home Assistant's Assist prompt that depend
// on the device asking, for the turn layer. Only while layer 4 is that
// prompt: with the own block there is nothing to add to.
func (e *Engine) deviceHintsFor(device string) string {
	if e.assist == nil || e.deviceHints == nil || e.assist() == "" {
		return ""
	}
	return e.deviceHints(device)
}

func clipProfile(p string, max int) string {
	p = strings.TrimSpace(p)
	if max <= 0 || len([]rune(p)) <= max {
		return p
	}
	return string([]rune(p)[:max])
}

// channelContext is the layer 7 line that tells the model where the request
// came from and how to read room references.
func (e *Engine) channelContext(ctx context.Context, req Request, sess store.Session, snap structure.Snapshot) string {
	t := e.texts(ctx)
	switch req.Channel {
	case config.ChannelVoice:
		if sess.AreaID != "" {
			name := snap.AreaNames[sess.AreaID]
			if name == "" {
				name = sess.AreaID
			}
			return fmt.Sprintf(t.voiceInArea, name)
		}
		return t.voiceNoArea
	case config.ChannelTelegram:
		return t.telegram
	default:
		return t.background
	}
}

// memories fetches the facts relevant to this message. Short words and
// filler words are dropped before the full-text search: "steht" or "bitte"
// match nearly every fact, and an unrelated fact in layer 7 is worse than
// none — the model takes it as a hint and builds an answer on it.
func (e *Engine) memories(ctx context.Context, text, person string) []string {
	limit := e.cfg.Memory.MaxFactsInLayer
	if limit <= 0 {
		return nil
	}
	var words []string
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 4 && !stopwords[strings.ToLower(w)] {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return nil
	}
	facts, err := e.store.SearchFacts(ctx, strings.Join(words, " "), store.Owners(person), limit)
	if err != nil {
		e.log.Warn("memory search failed", "error", err)
		return nil
	}
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		out = append(out, f.Text)
	}
	return out
}

// personProfile is the compacted personal memory of the speaker, if known.
func (e *Engine) personProfile(ctx context.Context, person string) string {
	if person == "" {
		return ""
	}
	p, _, err := e.store.Profile(ctx, person)
	if err != nil {
		e.log.Warn("could not read a personal profile", "person", person, "error", err)
		return ""
	}
	return clipProfile(p, e.cfg.Memory.MaxProfileChars)
}

// messageContext links a voice turn to messages announced on the same
// satellite moments ago, so "Was genau?" can be answered from the details.
// It also returns the ids of the messages that are still open, so the turn
// can acknowledge them once it is answered.
func (e *Engine) messageContext(ctx context.Context, deviceID string) (string, []string) {
	if deviceID == "" {
		return "", nil
	}
	a, err := e.store.RecentAnnouncement(ctx, deviceID, e.cfg.Notifier.FollowUpWindow.Duration())
	if err != nil {
		return "", nil
	}
	var b strings.Builder
	var open []string
	for _, id := range a.MessageIDs {
		n, err := e.store.GetMessage(ctx, id)
		if err != nil {
			continue
		}
		if n.State == store.MessageDelivered {
			open = append(open, n.ID)
		}
		b.WriteString("- [" + n.ID + "] " + n.Spoken)
		if n.Details != "" {
			b.WriteString(" Details: " + n.Details)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), open
}

// history loads layer 6.
func (e *Engine) history(ctx context.Context, key string) ([]llm.Message, error) {
	stored, err := e.store.ChatMessages(ctx, key)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, 0, len(stored))
	for _, m := range stored {
		msg := llm.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
		if len(m.ToolCalls) > 0 {
			if err := json.Unmarshal(m.ToolCalls, &msg.ToolCalls); err != nil {
				return nil, err
			}
		}
		out = append(out, msg)
	}
	return out, nil
}

func toStore(msgs []llm.Message) []store.ChatMessage {
	out := make([]store.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		sm := store.ChatMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
		if len(m.ToolCalls) > 0 {
			sm.ToolCalls, _ = json.Marshal(m.ToolCalls)
		}
		out = append(out, sm)
	}
	return out
}

// stopwords are words of four letters or more that carry no topic.
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		aber alle allem allen aller alles also auch bitte bist dann darf dass dein deine deinen deiner dem den denn der des dich diese diesem diesen dieser dieses doch dort durch eigentlich einem einen einer eines etwa etwas euch gerade gern gerne gibt geben haben habe hast hatte heute hier immer jetzt kann kannst können könntest machen mache mach mehr mein meine meinem meinen meiner merk merke merken mich mir mit nach nicht noch nochmal oder ohne schon sein seine sehr sind soll sollte steht stehen stand sagen sag schick schicke unser unsere unter viel vielleicht warum was weil weißt welche welcher welches wenn werden wieder wieso wird wissen wurde würde zeig zeige zeigen zwischen über
		about also does have just know please tell that there they this what when where which with would your`) {
		m[w] = true
	}
	return m
}()
