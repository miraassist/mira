package inbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miraassist/mira/internal/i18n"
	"github.com/miraassist/mira/internal/mailcal"
	"github.com/miraassist/mira/internal/retry"
	"github.com/miraassist/mira/internal/skills"
)

var berlin = func() *time.Location {
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return l
}()

// now is a fixed Tuesday; the tests do not depend on the real time.
var now = time.Date(2026, 11, 10, 10, 0, 0, 0, berlin)

type fakePaperless struct {
	mu       sync.Mutex
	docs     map[int]Doc
	texts    map[int]string
	tags     map[string]int
	textErr  error
	addErr   error
	calls    map[string]int
	addedTag map[int][]int
	unread   []int
}

func newPaperless() *fakePaperless {
	return &fakePaperless{docs: map[int]Doc{}, texts: map[int]string{}, tags: map[string]int{TagInbox: 620, TagRead: 621},
		calls: map[string]int{}, addedTag: map[int][]int{}}
}

func (p *fakePaperless) count(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[name]
}

func (p *fakePaperless) Doc(_ context.Context, id int) (Doc, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls["doc"]++
	d, ok := p.docs[id]
	if !ok {
		return Doc{}, &Gone{Err: fmt.Errorf("get_document %d: 404", id)}
	}
	return d, nil
}

func (p *fakePaperless) Text(_ context.Context, id int) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls["text"]++
	if p.textErr != nil {
		return "", p.textErr
	}
	return p.texts[id], nil
}

func (p *fakePaperless) Tag(_ context.Context, name string) (int, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls["tag"]++
	id, ok := p.tags[name]
	return id, ok, nil
}

func (p *fakePaperless) AddTag(_ context.Context, doc, tag int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls["add"]++
	if p.addErr != nil {
		return p.addErr
	}
	p.addedTag[doc] = append(p.addedTag[doc], tag)
	return nil
}

func (p *fakePaperless) Unread(_ context.Context, inbox, read int) ([]int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls["unread"]++
	if inbox != 620 || read != 621 {
		return nil, fmt.Errorf("tags %d, %d", inbox, read)
	}
	return p.unread, nil
}

type fakeModel struct {
	answer      string
	err         error
	instruction string
}

func (m *fakeModel) Ask(_ context.Context, _, instruction string, _ json.RawMessage) (string, error) {
	m.instruction = instruction
	return m.answer, m.err
}

type told struct{ receiver, text string }

type bed struct {
	s       *Service
	pl      *fakePaperless
	model   *fakeModel
	log     *bytes.Buffer
	asked   []Question
	told    []told
	open    map[string]bool
	owners  map[int]string
	kept    map[string][]string
	keptErr error
}

func newBed(t *testing.T) *bed {
	t.Helper()
	b := &bed{pl: newPaperless(), model: &fakeModel{}, log: &bytes.Buffer{}, open: map[string]bool{}, owners: map[int]string{},
		kept: map[string][]string{}}
	b.pl.docs[5444] = Doc{ID: 5444, Title: "Elternbrief", Created: time.Date(2026, 11, 9, 0, 0, 0, 0, berlin), Owner: 3}
	b.pl.texts[5444] = "Liebe Eltern, der Elternabend ist am 3. Dezember um 19:30 in der Aula."
	b.s = New(Options{Token: "inbox-secret", Paperless: b.pl, Model: b.model, Extract: "Nimm jeden Termin.",
		Person: func(owner string) (string, bool) {
			if owner == "anna" {
				return "anna", true
			}
			return "", false
		},
		Ask:   func(_ context.Context, q Question) error { b.asked = append(b.asked, q); return nil },
		Tell:  func(_ context.Context, r, text string) error { b.told = append(b.told, told{r, text}); return nil },
		Open:  func(_ context.Context, key string) bool { return b.open[key] },
		Learn: func(_ context.Context, owner int, name string) error { b.owners[owner] = name; return nil },
		Known: func(_ context.Context, entry, hash string) (bool, error) {
			return slices.Contains(b.kept[entry], hash), b.keptErr
		},
		Keep: func(_ context.Context, entry, hash string) error {
			if b.keptErr != nil {
				return b.keptErr
			}
			b.kept[entry] = append(b.kept[entry], hash)
			return nil
		},
		Owner: func(_ context.Context, owner int) (string, bool, error) {
			name, ok := b.owners[owner]
			return name, ok, nil
		},
		House: "house", Lang: i18n.DE, Location: berlin, Clock: func() time.Time { return now },
		Logger: slog.New(slog.NewTextHandler(b.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Policy: retry.Policy{Attempts: 4, First: time.Millisecond, Growth: retry.Exponential}})
	return b
}

// answer is the reading's answer: the summary, the plan's steps, the
// summary as the line, no expiry.
func (b *bed) answer(summary string, steps ...string) {
	b.reading(reading{Summary: summary, Plan: steps, Line: summary})
}

func (b *bed) reading(rd reading) {
	if rd.Plan == nil {
		rd.Plan = []string{}
	}
	raw, _ := json.Marshal(rd)
	b.model.answer = string(raw)
}

// appt is a step of the plan as the model words it.
func appt(title, day, start string) string {
	return strings.TrimSpace(title + ", " + day + " " + start)
}

func post(t *testing.T, s http.Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// --- step 1 -------------------------------------------------------------------

func TestIN01_WebhookIsTakenAtOnce(t *testing.T) {
	b := newBed(t)
	rec := post(t, b.s, "inbox-secret", `{"doc_id": "5444", "title": "Elternbrief", "owner": "anna"}`)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"queued":true`) {
		t.Fatalf("answer = %d %s", rec.Code, rec.Body)
	}
	if b.pl.count("text") != 0 || b.model.instruction != "" {
		t.Fatal("reading started before the answer")
	}
	if len(b.s.queue) != 1 || b.s.queue[0].doc != 5444 || b.s.queue[0].owner != "anna" {
		t.Fatalf("queue = %+v", b.s.queue)
	}
}

func TestIN02_OnlyTheInboxTokenOpensTheInbox(t *testing.T) {
	b := newBed(t)
	for _, token := range []string{"api-token", "wrong", ""} {
		if rec := post(t, b.s, token, `{"doc_id": 5444, "owner": "anna"}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: %d", token, rec.Code)
		}
	}
	if len(b.s.queue) != 0 {
		t.Fatalf("queued without the token: %+v", b.s.queue)
	}
	// That the inbox token opens no other endpoint: haconv's TestIN02.
}

func TestIN04_BrokenWebhookNamesItsField(t *testing.T) {
	b := newBed(t)
	for body, field := range map[string]string{
		`{"doc_id": "abc", "owner": "anna"}`: "doc_id",
		`{"doc_id": 5444}`:                   "owner",
		`nonsense`:                           "body",
	} {
		rec := post(t, b.s, "inbox-secret", body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), field) {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if !strings.Contains(b.log.String(), `body="{\"doc_id\": \"abc\"`) {
		t.Errorf("no log line with the body's start:\n%s", b.log)
	}
}

func TestIN05_ADocumentIsReadOnceAtATime(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	if !b.s.Enqueue(ctx, 5444, "anna") || b.s.Enqueue(ctx, 5444, "anna") {
		t.Fatal("a queued document was queued again")
	}
	b.open["paperless:5445"] = true
	if b.s.Enqueue(ctx, 5445, "anna") {
		t.Fatal("a document with an open thread was queued")
	}
	if !strings.Contains(b.log.String(), "still open") || !strings.Contains(b.log.String(), "already queued") {
		t.Errorf("no log lines saying why:\n%s", b.log)
	}
}

// --- step 2 -------------------------------------------------------------------

func TestIN06_OwnerBecomesThePerson(t *testing.T) {
	b := newBed(t)
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 1 || b.asked[0].Person != "anna" {
		t.Fatalf("asked = %+v", b.asked)
	}
}

func TestIN08_ALongTextIsCutAndSaid(t *testing.T) {
	b := newBed(t)
	b.pl.texts[5444] = strings.Repeat("x", 50000)
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.Read(context.Background(), 5444, "anna")
	if n := strings.Count(b.model.instruction, "x"); n != maxText {
		t.Fatalf("the reading got %d characters", n)
	}
	if len(b.asked) != 1 || !strings.Contains(b.asked[0].Text, "nur den Anfang") {
		t.Fatalf("asked = %+v", b.asked)
	}
	if !strings.Contains(b.log.String(), "length=50000") {
		t.Errorf("the log does not name the length:\n%s", b.log)
	}
}

func TestIN10_UnreachableIsTriedAgainThenLeft(t *testing.T) {
	def := New(Options{}).o.Policy
	if def.Attempts != 4 || def.Wait(1) != 2*time.Second || def.Wait(2) != 4*time.Second || def.Wait(3) != 8*time.Second {
		t.Fatalf("policy = %+v", def)
	}
	b := newBed(t)
	b.pl.textErr = retry.Transient(errors.New("timeout"))
	b.s.Read(context.Background(), 5444, "anna")
	if n := b.pl.count("text"); n != 4 {
		t.Fatalf("asked %d times", n)
	}
	if strings.Count(b.log.String(), "trying again") != 3 || !strings.Contains(b.log.String(), "next start takes it") {
		t.Errorf("log:\n%s", b.log)
	}
	if b.pl.count("add") != 0 || len(b.told) != 0 {
		t.Fatalf("marked read %d, told %+v", b.pl.count("add"), b.told)
	}
}

func TestIN11_ARefusedDocumentIsSaid(t *testing.T) {
	b := newBed(t)
	b.pl.textErr = retry.Fatal(errors.New("get_document_content: 403"))
	b.s.Read(context.Background(), 5444, "anna")
	if n := b.pl.count("text"); n != 1 {
		t.Fatalf("asked %d times", n)
	}
	if len(b.told) != 1 || b.told[0].receiver != "anna" || !strings.Contains(b.told[0].text, "nicht heran") {
		t.Fatalf("told = %+v", b.told)
	}
	if b.pl.count("add") != 0 {
		t.Fatal("a refused document was marked read")
	}
}

func TestIN12_AnAnswerThatIsNoJSONNamesItsStart(t *testing.T) {
	b := newBed(t)
	b.model.answer = "Gerne! Hier sind die Termine: Elternabend am 3. Dezember."
	b.s.Read(context.Background(), 5444, "anna")
	if !strings.Contains(b.log.String(), "Gerne! Hier sind die Termine") {
		t.Errorf("the log does not carry the answer's start:\n%s", b.log)
	}
	if len(b.told) != 1 || !strings.Contains(b.told[0].text, "nicht lesen") {
		t.Fatalf("told = %+v", b.told)
	}
	if b.pl.count("add") != 0 {
		t.Fatal("an unread document was marked read")
	}
}

// --- step 3 -------------------------------------------------------------------

func postfachSkill(t *testing.T) skills.Skill {
	t.Helper()
	loaded, problems := skills.Load([]string{"../../skills"}, map[string]map[string]any{Target: nil})
	if len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}
	for _, sk := range loaded {
		if sk.Name == Target {
			return sk
		}
	}
	t.Fatal("no skill postfach")
	return skills.Skill{}
}

func TestIN18_TheQuestionWaitsThreeDays(t *testing.T) {
	sk := postfachSkill(t)
	if d := sk.Target.TimeoutDuration(); d != 72*time.Hour {
		t.Fatalf("timeout = %s", d)
	}
	// Withdrawing the thread's message and the log line on expiry are the
	// threads' own (docs/spec/threads.md, End); the question is a message of
	// the thread (cmd/mira inboxAsk sets its ThreadID).
}

// --- step 4 -------------------------------------------------------------------

func TestIN19_AfterReadingMarkedReadTagStays(t *testing.T) {
	for name, answer := range map[string][]string{"with": {appt("Elternabend", "2026-12-03", "19:30")}, "without": nil} {
		b := newBed(t)
		b.answer("Elternbrief", answer...)
		b.s.Read(context.Background(), 5444, "anna")
		if got := b.pl.addedTag[5444]; len(got) != 1 || got[0] != 621 {
			t.Fatalf("%s steps: tags added %v", name, got)
		}
	}
}

func TestIN20_NotMarkedReadIsSaid(t *testing.T) {
	b := newBed(t)
	b.pl.addErr = retry.Fatal(errors.New("bulk_edit_documents: 403"))
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 1 {
		t.Fatal("the thread did not run")
	}
	if len(b.told) != 1 || !strings.Contains(b.told[0].text, "Mira gelesen selbst") {
		t.Fatalf("told = %+v", b.told)
	}
}

func TestIN22_AMissingTagIsNamed(t *testing.T) {
	b := newBed(t)
	delete(b.pl.tags, TagRead)
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.Read(context.Background(), 5444, "anna")
	b.s.Read(context.Background(), 5444, "anna")
	if n := strings.Count(b.log.String(), `tag="Mira gelesen"`); n != 1 {
		t.Fatalf("the tag is named %d times:\n%s", n, b.log)
	}
	if b.pl.count("add") != 0 {
		t.Fatal("marked read without the tag")
	}
	if rec := post(t, b.s, "inbox-secret", `{"doc_id": 7, "owner": "anna"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("webhook = %d", rec.Code)
	}
}

func TestEmptyTextIsSaidAndMarkedRead(t *testing.T) {
	b := newBed(t)
	b.pl.texts[5444] = "  "
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.told) != 1 || !strings.Contains(b.told[0].text, "keinen Text") || b.model.instruction != "" {
		t.Fatalf("told = %+v", b.told)
	}
	if got := b.pl.addedTag[5444]; len(got) != 1 {
		t.Fatalf("not marked read: %v", got)
	}
}

func TestRunReadsQueuedDocumentsInOrder(t *testing.T) {
	b := newBed(t)
	b.pl.docs[5445] = Doc{ID: 5445, Title: "Zweiter"}
	b.pl.texts[5445] = "Text"
	b.answer("Brief")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = b.s.Run(ctx); close(done) }()
	b.s.Enqueue(ctx, 5444, "anna")
	b.s.Enqueue(ctx, 5445, "anna")
	deadline := time.After(5 * time.Second)
	for b.pl.count("add") < 2 {
		select {
		case <-deadline:
			t.Fatal("the queue was not read")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	if b.s.Enqueue(context.Background(), 5444, "anna") != true {
		t.Fatal("a read document cannot be queued again")
	}
}

func TestIN21_AtStartTheUnreadAreCaughtUp(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	// A webhook named owner 3 "anna".
	b.answer("Elternbrief")
	b.s.Read(ctx, 5444, "anna")
	if b.owners[3] != "anna" {
		t.Fatalf("owners = %v", b.owners)
	}
	for _, id := range []int{5446, 5447, 5448} {
		b.pl.docs[id] = Doc{ID: id, Title: "Brief", Owner: 3}
	}
	b.pl.unread = []int{5446, 5447, 5448} // the two read ones the query leaves out
	b.s.CatchUp(ctx)
	b.s.CatchUp(ctx)
	if len(b.s.queue) != 3 || b.s.queue[0].owner != "anna" || b.s.queue[2].doc != 5448 {
		t.Fatalf("queue = %+v", b.s.queue)
	}
	if n := b.pl.count("unread"); n != 1 {
		t.Fatalf("queried %d times", n)
	}
}

func TestIN23_AnOwnerNotLearnedStaysUnread(t *testing.T) {
	b := newBed(t)
	b.pl.docs[5450] = Doc{ID: 5450, Title: "Brief", Owner: 9}
	b.pl.unread = []int{5450}
	b.s.CatchUp(context.Background())
	if len(b.s.queue) != 0 {
		t.Fatalf("queue = %+v", b.s.queue)
	}
	if !strings.Contains(b.log.String(), "doc=5450 owner_number=9") {
		t.Fatalf("log:\n%s", b.log)
	}
}

// --- the mail inbox -------------------------------------------------------------

func mail(entry, uid string) mailcal.Mail {
	return mailcal.Mail{Entry: entry, Sender: "schule@example.org", Subject: "Elternbrief",
		Date: time.Date(2026, 11, 9, 8, 0, 0, 0, berlin), Text: "<p>Elternabend am 3. Dezember um 19:30.</p>",
		ImapEntry: map[bool]string{true: "E1"}[uid != ""], UID: uid}
}

func (b *bed) runQueue(t *testing.T) {
	t.Helper()
	for len(b.s.queue) > 0 {
		j := b.s.queue[0]
		b.s.queue = b.s.queue[1:]
		b.s.readMail(context.Background(), *j.mail)
		delete(b.s.pending, j.key)
	}
}

func TestIN27_AnOlderIntegrationWithoutTheNewFields(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.EnqueueMail(ctx, mail("Postfach anna", ""))
	b.runQueue(t)
	if b.s.EnqueueMail(ctx, mail("Postfach anna", "")) {
		t.Fatal("the same mail was queued again")
	}
	if len(b.asked) != 1 || len(b.kept["Postfach anna"]) != 1 {
		t.Fatalf("asked %d, kept %v", len(b.asked), b.kept)
	}
}

func TestIN28_TheDeviceNameNamesThePerson(t *testing.T) {
	b := newBed(t)
	b.answer("Elternbrief der Grundschule", appt("Elternabend", "2026-12-03", "19:30"))
	if !b.s.EnqueueMail(context.Background(), mail("Postfach  anna", "7")) {
		t.Fatal("not queued")
	}
	b.runQueue(t)
	if len(b.asked) != 1 || b.asked[0].Person != "anna" || b.asked[0].Doc.Title != "Elternbrief" ||
		!strings.Contains(b.asked[0].Plan, "Mail von schule@example.org") {
		t.Fatalf("asked = %+v", b.asked)
	}
	if strings.Contains(b.model.instruction, "<p>") || !strings.Contains(b.model.instruction, "Mail von: schule@example.org") {
		t.Fatalf("instruction:\n%s", b.model.instruction)
	}
}

func TestIN29_WithoutALoginTheMailIsTheHouses(t *testing.T) {
	b := newBed(t)
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.EnqueueMail(context.Background(), mail("Postfach", "7"))
	b.runQueue(t)
	if len(b.asked) != 0 || len(b.told) != 1 || b.told[0].receiver != "house" || !strings.Contains(b.told[0].text, "Was soll ich damit machen?") {
		t.Fatalf("asked %+v, told %+v", b.asked, b.told)
	}
}

func TestIN30_AMailIsReadOnce(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	if !b.s.EnqueueMail(ctx, mail("Postfach anna", "7")) || b.s.EnqueueMail(ctx, mail("Postfach anna", "8")) {
		t.Fatal("a queued mail was queued again")
	}
	other := mail("Postfach anna", "9")
	other.Subject = "Ausflug"
	b.open["mail:E1:"+MailHash(other)] = true
	if b.s.EnqueueMail(ctx, other) {
		t.Fatal("a mail with an open thread was queued")
	}
	if !strings.Contains(b.log.String(), "already queued") || !strings.Contains(b.log.String(), "still open") {
		t.Errorf("no log lines:\n%s", b.log)
	}
}

func TestIN33_AMailReadBeforeIsNotReadAgain(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	b.answer("Elternbrief")
	b.s.EnqueueMail(ctx, mail("Postfach anna", "4"))
	b.runQueue(t)
	// A later mail moved out: HA reports this one again, on another uid.
	if b.s.EnqueueMail(ctx, mail("Postfach anna", "5")) {
		t.Fatal("a mail read before was queued again")
	}
	if !strings.Contains(b.log.String(), "a mail read before; dropped") || !strings.Contains(b.log.String(), "subject=Elternbrief") {
		t.Fatalf("log:\n%s", b.log)
	}
}

func TestIN34_ANewMailOnAnOldUIDIsRead(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	b.s.EnqueueMail(ctx, mail("Postfach anna", "3"))
	b.runQueue(t)
	next := mail("Postfach anna", "3")
	next.Subject, next.Text = "Terminvorschläge", "Montag oder Dienstag um 10 Uhr?"
	if !b.s.EnqueueMail(ctx, next) {
		t.Fatal("a new mail on an old uid was dropped")
	}
	b.runQueue(t)
	if len(b.asked) != 2 {
		t.Fatalf("asked %d", len(b.asked))
	}
}

func TestIN35_MailsAlikeButForTheDateAreTwo(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	b.answer("Elternbrief")
	b.s.EnqueueMail(ctx, mail("Postfach anna", "1"))
	b.runQueue(t)
	again := mail("Postfach anna", "2")
	again.Date = again.Date.Add(24 * time.Hour)
	if !b.s.EnqueueMail(ctx, again) {
		t.Fatal("a mail of another day was dropped")
	}
	same := mail("Postfach anna", "3")
	same.Date = same.Date.In(time.UTC) // the same instant in another zone
	if b.s.EnqueueMail(ctx, same) {
		t.Fatal("the same mail in another time zone was queued")
	}
}

func TestIN36_AMailWhoseReadingFailedIsNotKept(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	b.model.err = errors.New("model down")
	b.s.EnqueueMail(ctx, mail("Postfach anna", "7"))
	b.runQueue(t)
	if len(b.kept) != 0 {
		t.Fatalf("kept %v", b.kept)
	}
	if !b.s.EnqueueMail(ctx, mail("Postfach anna", "7")) {
		t.Fatal("the mail was not taken again")
	}
}

func TestIN37_MailsReadThatCannotBeLookedUpOrKept(t *testing.T) {
	b := newBed(t)
	ctx := context.Background()
	b.keptErr = errors.New("database locked")
	b.answer("Elternbrief", appt("Elternabend", "2026-12-03", "19:30"))
	if !b.s.EnqueueMail(ctx, mail("Postfach anna", "7")) {
		t.Fatal("not queued")
	}
	b.runQueue(t)
	if len(b.asked) != 1 {
		t.Fatal("the thread did not run")
	}
	for _, want := range []string{"could not look up the mails read before", "could not keep a read mail",
		"entry=E1", "database locked"} {
		if !strings.Contains(b.log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, b.log)
		}
	}
}

// --- step 9: the model writes the plan -----------------------------------------

func TestIN39_TheReadingsPlanBecomesTheThreadsPlan(t *testing.T) {
	b := newBed(t)
	steps := []string{"Elternabend, Do 3.12.2026 19:30, Aula", "Weihnachtsfeier, Sa 19.12.2026 15:00", "schulfrei, Di 22.12.2026 (ganztägig)"}
	b.reading(reading{Summary: "Elternbrief der Grundschule", Plan: steps, Line: "Elternbrief: drei Termine", Expires: "2026-12-19"})
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 1 {
		t.Fatalf("asked = %+v", b.asked)
	}
	q := b.asked[0]
	want := "Elternbrief der Grundschule (Paperless 5444): Elternbrief\n- [ ] " + strings.Join(steps, "\n- [ ] ")
	if q.Person != "anna" || q.Plan != want {
		t.Fatalf("plan:\n%s\nwant:\n%s", q.Plan, want)
	}
	if !q.Deadline.Equal(time.Date(2026, 12, 20, 0, 0, 0, 0, berlin)) {
		t.Fatalf("deadline = %s", q.Deadline)
	}
	if q.Text != "Elternbrief: drei Termine" {
		t.Fatalf("text = %q", q.Text)
	}
	// The target binds the thread to the person: skills/postfach.
	sk := postfachSkill(t)
	if sk.Target == nil || !sk.Target.Plan || sk.Target.Bound != "person" || sk.Inbox == nil {
		t.Fatalf("skill postfach: %+v", sk.Target)
	}
}

func TestIN40_AtMost20StepsTheRestNamed(t *testing.T) {
	b := newBed(t)
	var steps []string
	for i := 1; i <= 25; i++ {
		steps = append(steps, fmt.Sprintf("Termin %d, 2026-12-%02d", i, i))
	}
	b.answer("Terminliste", steps...)
	b.s.Read(context.Background(), 5444, "anna")
	q := b.asked[0]
	if n := strings.Count(q.Plan, "- [ ]"); n != 21 || !strings.HasSuffix(q.Plan, "- [ ] 5 weitere im Dokument") {
		t.Fatalf("%d steps, plan:\n%s", n, q.Plan)
	}
	if !strings.Contains(b.log.String(), "steps=25") {
		t.Errorf("the log does not name the 25:\n%s", b.log)
	}
	// A step longer than 200 characters is cut, with a log line.
	b = newBed(t)
	b.answer("Brief", strings.Repeat("y", 300))
	b.s.Read(context.Background(), 5444, "anna")
	if n := strings.Count(b.asked[0].Plan, "y"); n != maxStep || !strings.Contains(b.log.String(), "a step of the plan is cut") {
		t.Fatalf("%d characters kept", n)
	}
}

func TestIN41_AnExpiryThatDoesNotFit(t *testing.T) {
	for _, e := range []string{"2019-01-01", "morgen", "2031-01-01"} {
		b := newBed(t)
		b.reading(reading{Summary: "Brief", Plan: []string{"Termin"}, Line: "Brief", Expires: e})
		b.s.Read(context.Background(), 5444, "anna")
		if !b.asked[0].Deadline.IsZero() {
			t.Fatalf("%s: deadline %s", e, b.asked[0].Deadline)
		}
		if !strings.Contains(b.log.String(), "expiry does not fit") || !strings.Contains(b.log.String(), e) {
			t.Errorf("%s: no log line naming it:\n%s", e, b.log)
		}
	}
	// The thread's type's timeout is then its expiry: three days.
	if d := postfachSkill(t).Target.TimeoutDuration(); d != 72*time.Hour {
		t.Fatalf("timeout = %s", d)
	}
}

func TestIN42_WithoutAPersonThePlanGoesToTheHouse(t *testing.T) {
	b := newBed(t)
	b.reading(reading{Summary: "Elternbrief", Plan: []string{"Elternabend, Do 3.12.2026 19:30", "Feier, Sa 19.12.2026 15:00"},
		Line: "Elternbrief: zwei Termine"})
	b.s.Read(context.Background(), 5444, "scanner")
	if len(b.asked) != 0 || len(b.told) != 1 || b.told[0].receiver != "house" {
		t.Fatalf("asked %+v, told %+v", b.asked, b.told)
	}
	for _, want := range []string{"Elternbrief: zwei Termine", "Elternabend, Do 3.12.2026 19:30", "Feier, Sa 19.12.2026 15:00", "Was soll ich damit machen?"} {
		if !strings.Contains(b.told[0].text, want) {
			t.Errorf("message lacks %q: %s", want, b.told[0].text)
		}
	}
	if got := b.pl.addedTag[5444]; len(got) != 1 || got[0] != 621 {
		t.Fatalf("not marked read: %v", got)
	}
}

func TestIN43_AnEmptyPlanStartsNothing(t *testing.T) {
	b := newBed(t)
	b.answer("Werbung des Sportvereins")
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 0 || len(b.told) != 0 {
		t.Fatalf("asked %+v, told %+v", b.asked, b.told)
	}
	if !strings.Contains(b.log.String(), "nothing to act on") || !strings.Contains(b.log.String(), "Werbung des Sportvereins") {
		t.Errorf("log:\n%s", b.log)
	}
	if got := b.pl.addedTag[5444]; len(got) != 1 {
		t.Fatalf("not marked read: %v", got)
	}
}
