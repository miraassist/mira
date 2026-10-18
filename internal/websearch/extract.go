package websearch

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// parseResults reads DuckDuckGo's HTML results page: every result block
// with its link (result__a) and snippet (result__snippet); ads are left
// out. blocked reports a bot check instead of results.
func parseResults(body []byte) (results []Result, blocked bool) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			cls := attr(n, "class")
			if n.DataAtom == atom.Form && (attr(n, "id") == "challenge-form" || strings.Contains(attr(n, "action"), "anomaly")) {
				blocked = true
			}
			if hasClass(cls, "result") && !hasClass(cls, "result--ad") {
				if r, ok := result(n); ok {
					results = append(results, r)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if !blocked && len(results) == 0 && bytes.Contains(body, []byte("anomaly-modal")) {
		blocked = true
	}
	return results, blocked
}

// parseBing reads Bing's results page: every li.b_algo with the link in its
// h2 and the snippet in its caption.
func parseBing(body []byte) []Result {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil
	}
	var results []Result
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Li && hasClass(attr(n, "class"), "b_algo") {
			var r Result
			var inner func(c *html.Node)
			inner = func(c *html.Node) {
				if c.Type == html.ElementNode {
					switch {
					case c.DataAtom == atom.A && r.URL == "" && c.Parent != nil && c.Parent.DataAtom == atom.H2:
						r.Title = collapse(nodeText(c))
						r.URL = bingTarget(attr(c, "href"))
					case c.DataAtom == atom.P && r.Snippet == "":
						r.Snippet = collapse(nodeText(c))
					}
				}
				for k := c.FirstChild; k != nil; k = k.NextSibling {
					inner(k)
				}
			}
			inner(n)
			if r.URL != "" && r.Title != "" {
				results = append(results, r)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return results
}

// bingTarget unpacks Bing's redirect (bing.com/ck/a?…&u=a1<base64 of the
// address>); other links stay as they are.
func bingTarget(href string) string {
	u, err := url.Parse(href)
	if err != nil || !strings.HasSuffix(u.Host, "bing.com") {
		return href
	}
	packed := u.Query().Get("u")
	if !strings.HasPrefix(packed, "a1") {
		return href
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(packed[2:], "="))
	if err != nil || !strings.HasPrefix(string(raw), "http") {
		return href
	}
	return string(raw)
}

func result(block *html.Node) (Result, bool) {
	var r Result
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			cls := attr(n, "class")
			switch {
			case hasClass(cls, "result__a") && r.URL == "":
				r.Title = collapse(nodeText(n))
				r.URL = target(attr(n, "href"))
			case hasClass(cls, "result__snippet") && r.Snippet == "":
				r.Snippet = collapse(nodeText(n))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(block)
	if r.URL == "" || r.Title == "" || strings.Contains(r.URL, "duckduckgo.com/y.js") {
		return Result{}, false
	}
	return r, true
}

// target undoes DuckDuckGo's redirect link (//duckduckgo.com/l/?uddg=…).
func target(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") && u.Path == "/l/" {
		if t := u.Query().Get("uddg"); t != "" {
			return t
		}
	}
	if u.Scheme == "" && strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return href
}

// skip are elements whose text is not the page's content.
var skip = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true, atom.Svg: true,
	atom.Nav: true, atom.Header: true, atom.Footer: true, atom.Aside: true, atom.Form: true,
	atom.Button: true, atom.Select: true, atom.Iframe: true, atom.Canvas: true, atom.Dialog: true,
}

// block elements end a line in the extracted text.
var block = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Li: true, atom.Ul: true, atom.Ol: true, atom.Tr: true, atom.Table: true, atom.Br: true,
	atom.Blockquote: true, atom.Pre: true, atom.Dd: true, atom.Dt: true, atom.Figcaption: true,
}

// minMainChars: an <article> or <main> with less text than this is
// probably a teaser, and the whole body is used instead.
const minMainChars = 300

// extract returns a page's title and main text: <article> or <main> when
// they hold enough text, else the body, without scripts, navigation, header,
// footer and forms.
func extract(body []byte) (title, text string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", ""
	}
	var titleNode, bodyNode *html.Node
	var mains []*html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Title:
				if titleNode == nil {
					titleNode = n
				}
			case atom.Body:
				bodyNode = n
			case atom.Article, atom.Main:
				mains = append(mains, n)
			}
			if skip[n.DataAtom] {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if titleNode != nil {
		title = collapse(nodeText(titleNode))
	}
	best := ""
	for _, m := range mains {
		if t := lines(m); utf8.RuneCountInString(t) > utf8.RuneCountInString(best) {
			best = t
		}
	}
	if utf8.RuneCountInString(best) >= minMainChars {
		return title, best
	}
	root := bodyNode
	if root == nil {
		root = doc
	}
	return title, lines(root)
}

// lines is the readable text under n, one line per block, without the
// elements in skip.
func lines(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			if skip[n.DataAtom] || hasAttr(n, "hidden") || attr(n, "aria-hidden") == "true" {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && block[n.DataAtom] {
			b.WriteByte('\n')
		}
	}
	walk(n)
	var out []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = collapse(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func hasClass(cls, want string) bool {
	for _, c := range strings.Fields(cls) {
		if c == want {
			return true
		}
	}
	return false
}
