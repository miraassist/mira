// Package phone is phone_call (docs/spec/telefon.md): Mira calls a contact
// of the FRITZ!Box's phone book, a device in the house or a number from the
// satellite being spoken at.
package phone

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultFritzBox is the address a FRITZ!Box has out of the box.
const DefaultFritzBox = "192.168.178.1"

// tr064Port is where a FRITZ!Box answers TR-064.
const tr064Port = "49000"

const (
	onTel       = "urn:dslforum-org:service:X_AVM-DE_OnTel:1"
	onTelPath   = "/upnp/control/x_contact"
	readTimeout = 20 * time.Second
)

// Fritz is how to reach a FRITZ!Box's phone books.
type Fritz struct {
	// Address is the box's host name or IP; empty is DefaultFritzBox.
	Address        string
	User, Password string
	// Books are the phone books to read by name; empty is the first.
	Books []string
	// Client is for tests; nil is a client with a timeout.
	Client *http.Client
	// base overrides http://<address>:49000, for tests.
	base string
}

// Contact is a phone book entry.
type Contact struct {
	Name    string
	Numbers []Number
}

// Number is one of a contact's numbers.
type Number struct {
	Number string
	// Type is the FRITZ!Box's: home, mobile, work, intern, fax_work,
	// fixedline, label:…, or empty.
	Type string
	// Main is the box's main number of the contact (prio="1").
	Main bool
}

// Link is the box's own page, where its contacts are kept.
func (f Fritz) Link() string { return "http://" + f.address() }

func (f Fritz) address() string {
	if a := strings.TrimSpace(f.Address); a != "" {
		return a
	}
	return DefaultFritzBox
}

func (f Fritz) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: readTimeout}
}

func (f Fritz) url(path string) string {
	if f.base != "" {
		return f.base + path
	}
	return "http://" + net.JoinHostPort(f.address(), tr064Port) + path
}

// Read reads the chosen phone books: the names read and their contacts.
func (f Fritz) Read(ctx context.Context) ([]string, []Contact, error) {
	var list struct {
		IDs string `xml:"Body>GetPhonebookListResponse>NewPhonebookList"`
	}
	if err := f.soap(ctx, "GetPhonebookList", "", &list); err != nil {
		return nil, nil, err
	}
	type book struct{ name, url string }
	var books []book
	for _, id := range strings.Split(list.IDs, ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		var b struct {
			Name string `xml:"Body>GetPhonebookResponse>NewPhonebookName"`
			URL  string `xml:"Body>GetPhonebookResponse>NewPhonebookURL"`
		}
		if err := f.soap(ctx, "GetPhonebook", "<NewPhonebookID>"+id+"</NewPhonebookID>", &b); err != nil {
			return nil, nil, err
		}
		books = append(books, book{b.Name, b.URL})
		if len(f.Books) == 0 {
			break // the first one, unless others are asked for
		}
	}
	if len(books) == 0 {
		return nil, nil, errors.New("the FRITZ!Box has no phone book")
	}
	var chosen []book
	if len(f.Books) == 0 {
		chosen = books[:1]
	} else {
		var names []string
		for _, b := range books {
			names = append(names, b.name)
		}
		for _, want := range f.Books {
			found := false
			for _, b := range books {
				if strings.EqualFold(strings.TrimSpace(b.name), strings.TrimSpace(want)) {
					chosen, found = append(chosen, b), true
					break
				}
			}
			if !found {
				return nil, nil, fmt.Errorf("the FRITZ!Box has no phone book %q; it has: %s", want, strings.Join(names, ", "))
			}
		}
	}
	var read []string
	var contacts []Contact
	for _, b := range chosen {
		cs, err := f.fetch(ctx, b.url)
		if err != nil {
			return nil, nil, fmt.Errorf("phone book %q: %w", b.name, err)
		}
		read = append(read, b.name)
		contacts = merge(contacts, cs)
	}
	return read, contacts, nil
}

// fetch reads a phone book's XML from the address the box gave.
func (f Fritz) fetch(ctx context.Context, url string) ([]Contact, error) {
	if f.base != "" && strings.HasPrefix(url, "/") {
		url = f.base + url
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Books []struct {
			Contacts []struct {
				Name    string `xml:"person>realName"`
				Numbers []struct {
					Type   string `xml:"type,attr"`
					Prio   string `xml:"prio,attr"`
					Number string `xml:",chardata"`
				} `xml:"telephony>number"`
			} `xml:"contact"`
		} `xml:"phonebook"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("the phone book is no XML: %w", err)
	}
	var out []Contact
	for _, b := range doc.Books {
		for _, c := range b.Contacts {
			ct := Contact{Name: strings.TrimSpace(c.Name)}
			for _, n := range c.Numbers {
				if num := strings.TrimSpace(n.Number); num != "" {
					ct.Numbers = append(ct.Numbers, Number{Number: num, Type: n.Type, Main: n.Prio == "1"})
				}
			}
			if ct.Name != "" && len(ct.Numbers) > 0 {
				out = append(out, ct)
			}
		}
	}
	return out, nil
}

// merge adds contacts of another book; one with the same name and the same
// numbers is the same contact.
func merge(have, more []Contact) []Contact {
	for _, c := range more {
		dup := false
		for _, h := range have {
			if strings.EqualFold(h.Name, c.Name) && sameNumbers(h.Numbers, c.Numbers) {
				dup = true
				break
			}
		}
		if !dup {
			have = append(have, c)
		}
	}
	return have
}

func sameNumbers(a, b []Number) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if dialable(a[i].Number) != dialable(b[i].Number) {
			return false
		}
	}
	return true
}

// soap calls an action of X_AVM-DE_OnTel and decodes the answer.
func (f Fritz) soap(ctx context.Context, action, args string, into any) error {
	body := `<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` +
		action + ` xmlns:u="` + onTel + `">` + args + `</u:` + action + `></s:Body></s:Envelope>`
	send := func(auth string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url(onTelPath), strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
		req.Header.Set("SoapAction", onTel+"#"+action)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return f.client().Do(req)
	}
	resp, err := send("")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()
		auth, err := digest(challenge, f.User, f.Password, http.MethodPost, onTelPath)
		if err != nil {
			return err
		}
		if resp, err = send(auth); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("the FRITZ!Box refused the user or password")
	case resp.StatusCode != http.StatusOK:
		var fault struct {
			Desc string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
		}
		_ = xml.Unmarshal(raw, &fault)
		if fault.Desc != "" {
			return fmt.Errorf("the FRITZ!Box answered %d: %s", resp.StatusCode, fault.Desc)
		}
		return fmt.Errorf("the FRITZ!Box answered %d", resp.StatusCode)
	}
	return xml.Unmarshal(bytes.TrimSpace(raw), into)
}

// digest answers an HTTP digest challenge (RFC 2617, MD5, qop auth), as
// TR-064 asks for.
func digest(challenge, user, password, method, uri string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return "", errors.New("the FRITZ!Box asked for no digest login")
	}
	params := map[string]string{}
	for _, part := range splitParams(challenge[len("digest "):]) {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			params[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	h := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	ha1 := h(user + ":" + params["realm"] + ":" + password)
	ha2 := h(method + ":" + uri)
	cnonceRaw := make([]byte, 8)
	_, _ = rand.Read(cnonceRaw)
	cnonce := hex.EncodeToString(cnonceRaw)
	nc := "00000001"
	var response string
	qop := ""
	if strings.Contains(params["qop"], "auth") {
		qop = "auth"
		response = h(ha1 + ":" + params["nonce"] + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = h(ha1 + ":" + params["nonce"] + ":" + ha2)
	}
	out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", algorithm=MD5`,
		user, params["realm"], params["nonce"], uri, response)
	if qop != "" {
		out += fmt.Sprintf(`, qop=%s, nc=%s, cnonce="%s"`, qop, nc, cnonce)
	}
	if o := params["opaque"]; o != "" {
		out += `, opaque="` + o + `"`
	}
	return out, nil
}

// splitParams splits a challenge at commas outside quotes.
func splitParams(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case r == ',' && !quoted:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// dialable is a number as a phone dials it: digits, * and #, a leading +
// kept.
func dialable(n string) string {
	var b strings.Builder
	n = strings.TrimSpace(n)
	if strings.HasPrefix(n, "+") {
		b.WriteByte('+')
	}
	for _, r := range n {
		if (r >= '0' && r <= '9') || r == '*' || r == '#' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
