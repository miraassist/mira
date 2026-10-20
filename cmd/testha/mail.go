package main

import (
	"context"
	"flag"
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The mail calendar in the test HA (docs/spec/mail-kalender.md): the IMAP
// entry with the device "Pakete" reads the GreenMail mailbox pakete@test.local, and mail
// delivers the samples from testenv/mailsamples to it, one after the other.
//
//	go run ./cmd/testha mail                    all samples, in name order
//	go run ./cmd/testha mail -wait 20s 1 2      the samples starting with 1 and 2
//
// The samples are made up (no real data). Their dates are placeholders,
// filled in at delivery: {{DATE}} (now), {{TODAY}}, {{TOMORROW}},
// {{TOMORROW_WEEKDAY}}, {{UNTIL}} (in a week). Each gets a new Message-ID.

const (
	mailEntry = "Pakete"
	mailUser  = "pakete"
	mailAddr  = "pakete@test.local"
	smtpAddr  = "localhost:3025"
)

func mailCmd(ctx context.Context, dir string, args []string) error {
	fs := flag.NewFlagSet("mail", flag.ContinueOnError)
	wait := fs.Duration("wait", 30*time.Second, "pause between two mails, so Home Assistant reports each one")
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := loadEnv(dir)["TESTHA_ADMIN_TOKEN"]
	if !tokenWorks(ctx, token) {
		return fmt.Errorf("no working admin token, run `testha up` first")
	}
	if err := ensureIMAP(ctx, token); err != nil {
		return fmt.Errorf("IMAP entry: %w", err)
	}
	files, err := samples(filepath.Join(dir, "mailsamples"), fs.Args())
	if err != nil {
		return err
	}
	for i, f := range files {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(*wait):
			}
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if err := smtp.SendMail(smtpAddr, nil, "test@test.local", []string{mailAddr}, fill(raw, time.Now())); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		fmt.Println("delivered", filepath.Base(f))
	}
	return nil
}

// samples are the sample files, all or those starting with one of the
// prefixes, in name order.
func samples(dir string, prefixes []string) ([]string, error) {
	all, err := filepath.Glob(filepath.Join(dir, "*.eml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(all)
	if len(prefixes) == 0 {
		return all, nil
	}
	var out []string
	for _, p := range prefixes {
		n := len(out)
		for _, f := range all {
			if strings.HasPrefix(filepath.Base(f), p) {
				out = append(out, f)
			}
		}
		if len(out) == n {
			return nil, fmt.Errorf("no sample starting with %q in %s", p, dir)
		}
	}
	return out, nil
}

var weekdays = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}

// fill puts the dates into a sample and gives it a Message-ID of its own
// (Home Assistant reports a mail as new only when its id differs from the
// last one's), with CRLF line ends for SMTP.
func fill(raw []byte, now time.Time) []byte {
	tomorrow := now.AddDate(0, 0, 1)
	text := strings.NewReplacer(
		"{{DATE}}", now.Format(time.RFC1123Z),
		"{{TODAY}}", now.Format("02.01.2006"),
		"{{TOMORROW}}", tomorrow.Format("02.01.2006"),
		"{{TOMORROW_WEEKDAY}}", weekdays[tomorrow.Weekday()],
		"{{UNTIL}}", now.AddDate(0, 0, 7).Format("02.01.2006"),
	).Replace(string(raw))
	text = fmt.Sprintf("Message-ID: <%d@test.local>\n", now.UnixNano()) + text
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	return []byte(text)
}

// ensureIMAP creates the IMAP entry for the GreenMail mailbox once and names
// its device "Pakete" (the device's name is the switch for the mail
// calendar; the entry's own title does not count).
func ensureIMAP(ctx context.Context, token string) error {
	var entries []struct {
		Domain string `json:"domain"`
		Title  string `json:"title"`
	}
	if err := api(ctx, "GET", "/api/config/config_entries/entry", token, nil, &entries); err != nil {
		return err
	}
	found := false
	for _, e := range entries {
		// Older runs named the entry itself "Pakete".
		found = found || e.Domain == "imap" && (e.Title == mailEntry || e.Title == mailAddr || e.Title == mailUser)
	}
	if !found {
		var flow struct {
			FlowID string         `json:"flow_id"`
			Type   string         `json:"type"`
			Errors map[string]any `json:"errors"`
		}
		if err := api(ctx, "POST", "/api/config/config_entries/flow", token, map[string]any{"handler": "imap"}, &flow); err != nil {
			return err
		}
		// GreenMail in the same compose network, IMAPS with its own
		// certificate. It knows no UNDELETED, part of HA's default search.
		data := map[string]any{"username": mailUser, "password": mailUser, "server": "greenmail", "port": 3993,
			"verify_ssl": false, "search": "UNSEEN"}
		if err := api(ctx, "POST", "/api/config/config_entries/flow/"+flow.FlowID, token, data, &flow); err != nil {
			return err
		}
		if flow.Type != "create_entry" {
			return fmt.Errorf("imap setup ended with %q %v", flow.Type, flow.Errors)
		}
	}
	admin, stop, err := connect(ctx, token)
	if err != nil {
		return err
	}
	defer stop()
	// HA names the entry's device "IMAP (<user>)"; it appears with the
	// entry's sensor, a moment after the setup.
	for wait := 0; wait < 20; wait++ {
		devices, err := admin.Devices(ctx)
		if err != nil {
			return err
		}
		for _, d := range devices {
			if d.Name != "IMAP ("+mailUser+")" {
				continue
			}
			if d.NameByUser == mailEntry {
				return nil
			}
			_, err := admin.Call(ctx, map[string]any{"type": "config/device_registry/update", "device_id": d.ID, "name_by_user": mailEntry})
			if err == nil {
				fmt.Println("IMAP device", mailEntry, "set up")
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("no device %q in the test HA", "IMAP ("+mailUser+")")
}
