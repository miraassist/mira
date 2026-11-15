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
	askErr  error
	dropped []string
}

func newBed(t *testing.T) *bed {
	t.Helper()
	b := &bed{pl: newPaperless(), model: &fakeModel{}, log: &bytes.Buffer{}, open: map[string]bool{}, owners: map[int]string{},
		kept: map[string][]string{}}
	b.pl.docs[5444] = Doc{ID: 5444, Title: "Elternbrief", Created: time.Date(2026, 11, 9, 0, 0, 0, 0, berlin), Owner: 3}
	b.pl.texts[5444] = "Liebe Eltern, der Elternabend ist am 3. Dezember um 19:30 in der Aula."
	b.s = New(Options{Token: "inbox-secret", Paperless: b.pl, Model: b.model, Extract: "Nimm jeden Termin.",
		Kinds: func() map[string][]string {
			return map[string][]string{
				"appointment": {"title", "start", "end", "day", "day_after", "location", "description", "proposals", "calendar"},
				"todo":        {"item", "list", "due", "description"},
			}
		},
		Drop: func(_ context.Context, key string) { b.dropped = append(b.dropped, key) },
		Person: func(owner string) (string, bool) {
			if owner == "anna" {
				return "anna", true
			}
			return "", false
		},
		Ask: func(_ context.Context, q Question) error {
			if b.askErr != nil && len(b.asked) > 0 {
				return b.askErr
			}
			b.asked = append(b.asked, q)
			return nil
		},
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

// answer is the reading's answer: the summary and an appointment item per
// line, its title the line, no expiry.
func (b *bed) answer(summary string, lines ...string) {
	rd := reading{Summary: summary, Items: []item{}}
	for _, l := range lines {
		rd.Items = append(rd.Items, item{Kind: "appointment", Line: l, Fields: map[string]string{"title": l}})
	}
	b.reading(rd)
}

func (b *bed) reading(rd reading) {
	if rd.Items == nil {
		rd.Items = []item{}
	}
	raw, _ := json.Marshal(rd)
	b.model.answer = string(raw)
}

// appt is an appointment's line as the model words it.
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
	if len(b.asked) != 1 || !strings.Contains(b.asked[0].Line, "nur den Anfang") {
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

// IN-18 (the question waits three days) dropped with step 10: each item's thread expires by its own expiry.

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
		!strings.Contains(b.asked[0].Doc.Ref, "Mail von schule@example.org") {
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

// --- step 9, changed by step 10 ---------------------------------------------
// IN-39 and IN-40 (the plan per mail) dropped with step 10 (decision 29).

func TestIN41_AnExpiryThatDoesNotFitOrNone(t *testing.T) {
	for _, e := range []string{"2019-01-01", "morgen", "2031-01-01", ""} {
		b := newBed(t)
		b.reading(reading{Summary: "Brief", Items: []item{{Kind: "todo", Line: "Brief", Expires: e, Fields: map[string]string{"item": "Rückmeldung"}}}})
		b.s.Read(context.Background(), 5444, "anna")
		if got := b.asked[0].Deadline; !got.Equal(now.Add(24 * time.Hour)) {
			t.Fatalf("%q: deadline %s", e, got)
		}
		if logged := strings.Contains(b.log.String(), "expiry does not fit"); logged != (e != "") {
			t.Errorf("%q: log line %v:\n%s", e, logged, b.log)
		}
	}
	// A time is taken as it is; a day until its end.
	b := newBed(t)
	b.reading(reading{Summary: "Brief", Items: []item{
		{Kind: "appointment", Line: "Elternabend", Expires: "2026-11-11 12:00", Fields: map[string]string{"title": "Elternabend"}},
		{Kind: "todo", Line: "Rückmeldung", Expires: "2026-11-16", Fields: map[string]string{"item": "Rückmeldung"}}}})
	b.s.Read(context.Background(), 5444, "anna")
	if got := b.asked[0].Deadline; !got.Equal(time.Date(2026, 11, 11, 12, 0, 0, 0, berlin)) {
		t.Fatalf("deadline %s", got)
	}
	if got := b.asked[1].Deadline; !got.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, berlin)) {
		t.Fatalf("deadline %s", got)
	}
}

func TestIN42_WithoutAPersonThePlanGoesToTheHouse(t *testing.T) {
	b := newBed(t)
	b.answer("Elternbrief", "Elternbrief: Elternabend Do 3.12. 19:30", "Elternbrief: Feier Sa 19.12. 15:00")
	b.s.Read(context.Background(), 5444, "scanner")
	if len(b.asked) != 0 || len(b.told) != 1 || b.told[0].receiver != "house" {
		t.Fatalf("asked %+v, told %+v", b.asked, b.told)
	}
	for _, want := range []string{"Elternbrief: Elternabend Do 3.12. 19:30", "Elternbrief: Feier Sa 19.12. 15:00", "Was soll ich damit machen?"} {
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

// --- step 10: kinds by meaning --------------------------------------------------

func TestIN45_TheReadingGivesItemsByMeaning(t *testing.T) {
	b := newBed(t)
	b.reading(reading{Summary: "Elternbrief der Grundschule", Items: []item{
		{Kind: "appointment", Line: "Elternbrief: Elternabend Do 3.12. 19:30, Aula", Expires: "2026-12-03 19:30",
			Fields: map[string]string{"title": "Elternabend", "proposals": "2026-12-03 19:30–20:30 · Aula",
				"description": "Aus Paperless 5444: Elternbrief der Grundschule"}},
		{Kind: "todo", Line: "Elternbrief: Rückmeldung Ausflug bis Fr 28.11.", Expires: "2026-11-28",
			Fields: map[string]string{"item": "Rückmeldung Ausflug", "due": "2026-11-28"}}}})
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 2 {
		t.Fatalf("asked = %+v", b.asked)
	}
	a, d := b.asked[0], b.asked[1]
	if a.Kind != "appointment" || a.Person != "anna" || a.Fields["title"] != "Elternabend" ||
		a.Fields["proposals"] != "2026-12-03 19:30–20:30 · Aula" || a.Line != "Elternbrief: Elternabend Do 3.12. 19:30, Aula" ||
		a.Doc.Key != "paperless:5444" {
		t.Fatalf("appointment = %+v", a)
	}
	if d.Kind != "todo" || d.Fields["item"] != "Rückmeldung Ausflug" || d.Fields["due"] != "2026-11-28" ||
		!d.Deadline.Equal(time.Date(2026, 11, 29, 0, 0, 0, 0, berlin)) {
		t.Fatalf("todo = %+v", d)
	}
	if !strings.Contains(b.model.instruction, "Arten: appointment, todo") {
		t.Fatalf("instruction:\n%s", b.model.instruction)
	}
}

func TestIN49_ProposalsToChooseOneAreOneAppointment(t *testing.T) {
	b := newBed(t)
	b.reading(reading{Summary: "Praxis", Items: []item{{Kind: "appointment", Line: "Praxis Dr. Kaya schlägt drei Termine vor",
		Fields: map[string]string{"title": "Kontrolle", "proposals": "2026-11-17 09:00–09:30\n2026-11-18 14:30–15:00\n2026-11-20 11:00–11:30"}}}})
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 1 || strings.Count(b.asked[0].Fields["proposals"], "\n") != 2 {
		t.Fatalf("asked = %+v", b.asked)
	}
}

func TestIN52_ItemsThatDoNotFitAreDroppedTheOthersGoOn(t *testing.T) {
	b := newBed(t)
	b.reading(reading{Summary: "Rechnung", Items: []item{
		{Kind: "rechnung", Line: "Stadtwerke: 84 Euro", Fields: map[string]string{"betrag": "84"}},
		{Kind: "todo", Line: "Stadtwerke: bezahlen bis 30.11.", Fields: map[string]string{"item": "Stadtwerke bezahlen", "due": "2026-11-30"}}}})
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 1 || b.asked[0].Kind != "todo" {
		t.Fatalf("asked = %+v", b.asked)
	}
	if !strings.Contains(b.log.String(), "kind no switched-on skill takes") || !strings.Contains(b.log.String(), "rechnung") ||
		!strings.Contains(b.log.String(), "paperless:5444") {
		t.Fatalf("log:\n%s", b.log)
	}
}

func TestIN55_AFieldTheTakingSkillDoesNotKnowIsDropped(t *testing.T) {
	b := newBed(t)
	b.reading(reading{Summary: "Brief", Items: []item{{Kind: "todo", Line: "Formular",
		Fields: map[string]string{"item": "Formular", "due": "2026-11-28", "priority": "hoch"}}}})
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.asked) != 1 || b.asked[0].Fields["priority"] != "" || b.asked[0].Fields["due"] != "2026-11-28" {
		t.Fatalf("asked = %+v", b.asked)
	}
	if !strings.Contains(b.log.String(), "field=priority") {
		t.Fatalf("log:\n%s", b.log)
	}
}

func TestIN56_TheReadingIsOfferedOnlyTheKindsASwitchedOnSkillTakes(t *testing.T) {
	b := newBed(t)
	b.s.o.Kinds = func() map[string][]string { return map[string][]string{"appointment": {"title", "proposals"}} }
	b.reading(reading{Summary: "Brief", Items: []item{{Kind: "todo", Line: "Formular", Fields: map[string]string{"item": "Formular"}}}})
	b.s.Read(context.Background(), 5444, "anna")
	if !strings.Contains(b.model.instruction, "Arten: appointment\n") || len(b.asked) != 0 ||
		!strings.Contains(b.log.String(), "kind no switched-on skill takes") {
		t.Fatalf("instruction:\n%s\nasked %+v", b.model.instruction, b.asked)
	}
}

// An item that cannot be asked drops the document's threads; the document
// stays unread and is read again (docs/spec/inbox.md, section 8, Errors).
func TestAnItemThatCannotBeAskedLeavesTheDocumentUnread(t *testing.T) {
	b := newBed(t)
	b.askErr = errors.New("database locked")
	b.answer("Elternbrief", "Elternabend", "Feier")
	b.s.Read(context.Background(), 5444, "anna")
	if len(b.dropped) != 1 || b.dropped[0] != "paperless:5444" {
		t.Fatalf("dropped = %v", b.dropped)
	}
	if got := b.pl.addedTag[5444]; len(got) != 0 {
		t.Fatalf("marked read: %v", got)
	}
	if !strings.Contains(b.log.String(), "database locked") {
		t.Fatalf("log:\n%s", b.log)
	}
}
