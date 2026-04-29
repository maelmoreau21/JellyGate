package handlers

import (
	"html/template"
	"regexp"
	"strings"
)

var (
	productBoldPattern   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	productItalicPattern = regexp.MustCompile(`\*([^*]+)\*`)
)

func renderProductMarkdownHTML(raw string) template.HTML {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var b strings.Builder
	inList := false
	flushList := func() {
		if inList {
			b.WriteString("</ul>")
			inList = false
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushList()
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "### "):
			flushList()
			b.WriteString(`<h4>`)
			b.WriteString(productInlineMarkdown(trimmed[4:]))
			b.WriteString(`</h4>`)
		case strings.HasPrefix(trimmed, "## "):
			flushList()
			b.WriteString(`<h3>`)
			b.WriteString(productInlineMarkdown(trimmed[3:]))
			b.WriteString(`</h3>`)
		case strings.HasPrefix(trimmed, "# "):
			flushList()
			b.WriteString(`<h2>`)
			b.WriteString(productInlineMarkdown(trimmed[2:]))
			b.WriteString(`</h2>`)
		case strings.HasPrefix(trimmed, "- "):
			if !inList {
				b.WriteString("<ul>")
				inList = true
			}
			b.WriteString(`<li>`)
			b.WriteString(productInlineMarkdown(trimmed[2:]))
			b.WriteString(`</li>`)
		default:
			flushList()
			b.WriteString(`<p>`)
			b.WriteString(productInlineMarkdown(trimmed))
			b.WriteString(`</p>`)
		}
	}
	flushList()

	return template.HTML(b.String())
}

func productInlineMarkdown(raw string) string {
	escaped := template.HTMLEscapeString(strings.TrimSpace(raw))
	escaped = productBoldPattern.ReplaceAllString(escaped, "<strong>$1</strong>")
	escaped = productItalicPattern.ReplaceAllString(escaped, "<em>$1</em>")
	return escaped
}
