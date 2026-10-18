// Package websearch is Mira's own web search (docs/spec/websuche.md): the
// tools web_search, which asks DuckDuckGo's HTML results page, and web_page,
// which reads the main text of one page. No browser, no key. Both only
// read, and what they return is untrusted third-party text, so the guard
// locks critical actions for the rest of the turn once they ran.
package websearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html/charset"

	"github.com/miraassist/mira/internal/i18n"
)

const (
	ToolSearch = "web_search"
	ToolPage   = "web_page"

	defaultSearchURL = "https://html.duckduckgo.com/html/"
	// fallbackURL: Bing's results page, asked when DuckDuckGo shows a bot
	// check (it does after a few searches in a row from one address).
	fallbackURL = "https://www.bing.com/search"
	// userAgent: DuckDuckGo's HTML page answers a plain browser string.
	userAgent    = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"
	maxResults   = 5
	maxPageBytes = 2 << 20
	// maxPageChars keeps a page within what one tool result may hold.
	maxPageChars = 6000
	timeout      = 10 * time.Second
)

// ErrBlocked: DuckDuckGo showed a bot check instead of results.
var ErrBlocked = errors.New("the search engine shows a bot check instead of results")

// errPrivate: a page in the own network, which web_page does not read.
var errPrivate = errors.New("addresses in the local network are not read")

type Options struct {
	Lang i18n.Lang
	// Enabled says whether the tools are offered; nil means always.
	Enabled func() bool
	// HTTP and SearchURL are for tests; AllowPrivate lets tests read
	// pages on the loopback interface.
	HTTP         *http.Client
	SearchURL    string
	FallbackURL  string
	AllowPrivate bool
	Logger       *slog.Logger
}

type Search struct {
	opts   Options
	client *http.Client
	log    *slog.Logger
}

func New(opts Options) *Search {
	if opts.SearchURL == "" {
		opts.SearchURL = defaultSearchURL
	}
	if opts.FallbackURL == "" {
		opts.FallbackURL = fallbackURL
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	client := opts.HTTP
	if client == nil {
		dialer := &net.Dialer{Timeout: timeout}
		if !opts.AllowPrivate {
			// Checked on the address actually dialled, so neither a name
			// that resolves into the home network nor a redirect there gets
			// through.
			dialer.Control = func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip == nil || private(ip) {
					return errPrivate
				}
				return nil
			}
		}
		client = &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: timeout},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return checkScheme(req.URL)
			},
		}
	}
	return &Search{opts: opts, client: client, log: opts.Logger}
}

func (s *Search) enabled() bool {
	return s.opts.Enabled == nil || s.opts.Enabled()
}

// Result is one hit.
type Result struct {
	Title, URL, Snippet string
}

// Query asks DuckDuckGo and, when that is blocked or fails, Bing; it
// returns up to five hits.
func (s *Search) Query(ctx context.Context, query string) ([]Result, error) {
	results, err := s.duckDuckGo(ctx, query)
	if err == nil {
		return results, nil
	}
	s.log.Warn("web search: DuckDuckGo did not answer, asking Bing", "error", err)
	fallback, ferr := s.bing(ctx, query)
	if ferr != nil {
		s.log.Warn("web search: Bing did not answer either", "error", ferr)
		return nil, err
	}
	return fallback, nil
}

func (s *Search) duckDuckGo(ctx context.Context, query string) ([]Result, error) {
	form := url.Values{"q": {query}, "kl": {region(s.opts.Lang)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.opts.SearchURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", acceptLanguage(s.opts.Lang))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 202 is how DuckDuckGo sends its bot check.
		if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, ErrBlocked
		}
		return nil, fmt.Errorf("search: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, err
	}
	results, blocked := parseResults(body)
	if len(results) == 0 && blocked {
		return nil, ErrBlocked
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

func (s *Search) bing(ctx context.Context, query string) ([]Result, error) {
	q := url.Values{"q": {query}, "setlang": {string(s.opts.Lang)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.opts.FallbackURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", acceptLanguage(s.opts.Lang))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bing: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, err
	}
	results := parseBing(body)
	if len(results) == 0 {
		return nil, errors.New("bing: no results on the page")
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// Page is a page's title and main text.
type Page struct {
	Title, Text string
	Cut         bool
}

// Read loads one page and extracts its main text.
func (s *Search) Read(ctx context.Context, raw string) (Page, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Page{}, fmt.Errorf("not an address: %w", err)
	}
	if err := checkScheme(u); err != nil {
		return Page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9")
	req.Header.Set("Accept-Language", acceptLanguage(s.opts.Lang))
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, errPrivate) {
			return Page{}, errPrivate
		}
		return Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Page{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	ctype := resp.Header.Get("Content-Type")
	plain := strings.HasPrefix(ctype, "text/plain")
	if !plain && ctype != "" && !strings.Contains(ctype, "html") {
		return Page{}, fmt.Errorf("not a web page but %s", strings.Split(ctype, ";")[0])
	}
	body, err := charset.NewReader(io.LimitReader(resp.Body, maxPageBytes), ctype)
	if err != nil {
		return Page{}, err
	}
	raw2, err := io.ReadAll(body)
	if err != nil {
		return Page{}, err
	}
	var p Page
	if plain {
		var keep []string
		for _, l := range strings.Split(string(raw2), "\n") {
			if l = collapse(l); l != "" {
				keep = append(keep, l)
			}
		}
		p.Text = strings.Join(keep, "\n")
	} else {
		p.Title, p.Text = extract(raw2)
	}
	if r := []rune(p.Text); len(r) > maxPageChars {
		p.Text, p.Cut = strings.TrimSpace(string(r[:maxPageChars]))+" …", true
	}
	return p, nil
}

func checkScheme(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https addresses are read, not %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("the address has no host")
	}
	return nil
}

// private: loopback, private, link-local and similar addresses.
func private(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		// carrier-grade NAT, 100.64.0.0/10
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64)
}

func region(l i18n.Lang) string {
	if l == i18n.EN {
		return "us-en"
	}
	return "de-de"
}

func acceptLanguage(l i18n.Lang) string {
	if l == i18n.EN {
		return "en-US,en;q=0.9"
	}
	return "de-DE,de;q=0.9,en;q=0.5"
}
