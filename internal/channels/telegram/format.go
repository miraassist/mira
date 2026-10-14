package telegram

import (
	"html"
	"regexp"
	"strings"
)

// MaxMessageLen is Telegram's limit, minus headroom for HTML tags that the
// conversion adds after splitting.
const MaxMessageLen = 3500

// ToHTML converts the Markdown a model typically writes into Telegram's HTML
// subset. HTML is used instead of Telegram's Markdown mode because entity ids
// like light.kueche_decke are full of underscores, which Telegram's Markdown
// parser rejects.
func ToHTML(md string) string {
	var out strings.Builder
	lines := strings.Split(md, "\n")
	inCode := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCode {
				out.WriteString("</pre>")
			} else {
				out.WriteString("<pre>")
			}
			inCode = !inCode
			if i < len(lines)-1 && inCode {
				continue
			}
		} else if inCode {
			out.WriteString(html.EscapeString(line))
		} else {
			out.WriteString(inline(line))
		}
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	if inCode {
		out.WriteString("</pre>")
	}
	return out.String()
}

var (
	reHeading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	reCode    = regexp.MustCompile("`([^`]+)`")
	reBold    = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	reItalic  = regexp.MustCompile(`(^|[^\w*])\*([^*\s][^*]*?)\*($|[^\w*])`)
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	reBullet  = regexp.MustCompile(`^(\s*)[-*]\s+`)
)

func inline(line string) string {
	heading := false
	if m := reHeading.FindStringSubmatch(line); m != nil {
		line, heading = m[1], true
	}
	line = reBullet.ReplaceAllString(line, "$1• ")

	// Code spans are cut out first so nothing inside them is formatted.
	var codes []string
	line = reCode.ReplaceAllStringFunc(line, func(s string) string {
		codes = append(codes, s[1:len(s)-1])
		return "\x00" + string(rune('0'+len(codes)-1)) + "\x00"
	})
	line = html.EscapeString(line)
	line = reLink.ReplaceAllString(line, `<a href="$2">$1</a>`)
	line = reBold.ReplaceAllStringFunc(line, func(s string) string {
		return "<b>" + s[2:len(s)-2] + "</b>"
	})
	line = reItalic.ReplaceAllString(line, "$1<i>$2</i>$3")
	for i, c := range codes {
		line = strings.Replace(line, "\x00"+string(rune('0'+i))+"\x00", "<code>"+html.EscapeString(c)+"</code>", 1)
	}
	if heading {
		line = "<b>" + line + "</b>"
	}
	return line
}

// Split cuts a long answer into chunks under the limit, preferring paragraph
// and line breaks so formatting spans are rarely cut apart.
func Split(text string, limit int) []string {
	var out []string
	for len(text) > limit {
		cut := strings.LastIndex(text[:limit], "\n\n")
		if cut < limit/2 {
			cut = strings.LastIndex(text[:limit], "\n")
		}
		if cut < limit/2 {
			cut = strings.LastIndex(text[:limit], " ")
		}
		if cut <= 0 {
			cut = limit
			for cut > 0 && !isRuneStart(text[cut]) {
				cut--
			}
		}
		out = append(out, strings.TrimRight(text[:cut], " \n"))
		text = strings.TrimLeft(text[cut:], " \n")
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, text)
	}
	return out
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
