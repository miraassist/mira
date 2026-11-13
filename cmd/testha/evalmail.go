package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/miraassist/mira/internal/inbox"
	"github.com/miraassist/mira/internal/mailcal"
)

// A mail into Mira's inbox in an eval (docs/spec/evals.md, The inbox): the
// runner posts it to the eval service's /v1/mail as the integration does, and
// waits until the inbox has read it. IMAP and the integration stay out: the
// eval tests the models, not the way in.

// StepMail is a mail a step puts into the inbox. Its texts may name days
// relative to today: {{+7}} becomes "Donnerstag, 19. November 2026".
type StepMail struct {
	// Device is the IMAP device's name; "Postfach anna" when empty.
	Device  string `yaml:"device"`
	From    string `yaml:"from"`
	Subject string `yaml:"subject"`
	Text    string `yaml:"text"`
}

// mailEntry is the IMAP entry an eval's mails come from.
const mailEntryEval = "eval"

// mailRead is how long the runner waits for the inbox to read a mail: a
// safety net only, the signal is the inbox's log line.
const mailRead = 2 * time.Minute

var relDay = regexp.MustCompile(`\{\{\+(\d+)\}\}`)

var (
	weekdaysDE = []string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	monthsDE   = []string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September",
		"Oktober", "November", "Dezember"}
)

// fillDays replaces {{+N}} with the German date N days from today.
func fillDays(s string) string {
	return relDay.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(relDay.FindStringSubmatch(m)[1])
		d := time.Now().AddDate(0, 0, n)
		return fmt.Sprintf("%s, %d. %s %d", weekdaysDE[d.Weekday()], d.Day(), monthsDE[d.Month()-1], d.Year())
	})
}

// sendMail posts the step's mail to the eval service and waits for the
// inbox's log line that it is done with it. The mail's date is now, so a
// run never meets the mail of an earlier run among the mails read.
func (r *evalRun) sendMail(ctx context.Context, sm StepMail) error {
	device := sm.Device
	if device == "" {
		device = "Postfach anna"
	}
	m := mailcal.Mail{Entry: device, Sender: sm.From, Subject: fillDays(sm.Subject), Text: fillDays(sm.Text),
		Date: time.Now().Truncate(time.Second), ImapEntry: mailEntryEval, UID: "1"}
	key := "key=mail:" + mailEntryEval + ":" + inbox.MailHash(m)
	from, err := logSize(r.agentLog)
	if err != nil {
		return err
	}
	var out struct {
		Queued bool `json:"queued"`
	}
	body := map[string]any{"entry": m.Entry, "sender": m.Sender, "subject": m.Subject,
		"date": m.Date.Format(time.RFC3339), "text": m.Text, "imap_entry": m.ImapEntry, "uid": m.UID}
	if err := r.agent(ctx, "POST", "/v1/mail", body, &out); err != nil {
		return err
	}
	if !out.Queued {
		return fmt.Errorf("the inbox did not queue the mail %q", m.Subject)
	}
	return waitLog(ctx, r.agentLog, from, mailRead, func(line string) (bool, error) {
		if !strings.Contains(line, "component=inbox") || !strings.Contains(line, key) {
			return false, nil
		}
		switch {
		case strings.Contains(line, "msg=done"):
			return true, nil
		case strings.Contains(line, "could not be read"):
			return true, fmt.Errorf("the inbox could not read the mail %q: %s", m.Subject, line)
		}
		return false, nil
	})
}

func logSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// waitLog reads the log from an offset on, line by line as it grows, until
// match says done or the wait is over.
func waitLog(ctx context.Context, path string, from int64, wait time.Duration, match func(string) (bool, error)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return err
	}
	rd := bufio.NewReader(f)
	deadline := time.Now().Add(wait)
	var part string
	for {
		line, err := rd.ReadString('\n')
		part += line
		if err == nil {
			if done, err := match(part); done || err != nil {
				return err
			}
			part = ""
			continue
		}
		if err != io.EOF {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no word of the inbox in %s (%s)", wait, path)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// PlanCheck wants a running plan of a target (a thread with a plan), or
// none with Not; Finished wants it finished.
type PlanCheck struct {
	Target   string `yaml:"target"`
	Finished bool   `yaml:"finished"`
	Not      bool   `yaml:"not"`
}

func (r *evalRun) checkPlan(ctx context.Context, c PlanCheck) string {
	var out struct {
		Plans []struct {
			Target   string `json:"target"`
			Finished bool   `json:"finished"`
			Done     int    `json:"done"`
			Steps    int    `json:"steps"`
		} `json:"plans"`
	}
	if err := r.agent(ctx, "GET", "/v1/ui/plans", nil, &out); err != nil {
		return err.Error()
	}
	var found []string
	for _, p := range out.Plans {
		if p.Target != c.Target {
			continue
		}
		if c.Not && !p.Finished {
			return fmt.Sprintf("a plan of %s is running (%d of %d done)", c.Target, p.Done, p.Steps)
		}
		if !c.Not && (!c.Finished || p.Finished) {
			return ""
		}
		found = append(found, fmt.Sprintf("%d of %d done", p.Done, p.Steps))
	}
	switch {
	case c.Not:
		return ""
	case c.Finished && len(found) > 0:
		return fmt.Sprintf("the plan of %s is not finished (%s)", c.Target, strings.Join(found, ", "))
	}
	return fmt.Sprintf("no plan of %s", c.Target)
}

// NoMessage wants no message for a receiver.
type NoMessage struct {
	Receiver string `yaml:"receiver"`
}

func (r *evalRun) checkNoMessage(ctx context.Context, c NoMessage) string {
	var msgs struct {
		Items []map[string]any `json:"items"`
	}
	if err := r.agent(ctx, "GET", "/v1/ui/messages", nil, &msgs); err != nil {
		return err.Error()
	}
	for _, m := range msgs.Items {
		if fmt.Sprint(m["receiver"]) == c.Receiver {
			return fmt.Sprintf("a message for %s: %q", c.Receiver, fmt.Sprint(m["spoken"]))
		}
	}
	return ""
}
