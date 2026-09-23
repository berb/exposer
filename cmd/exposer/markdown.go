package main

import (
	"regexp"
	"strings"
)

// A caption is Markdown (R-4). The site renders it, but three places need its
// words alone: alt text, which is read aloud (D-9), the page title behind an
// untitled photograph, and the description handed to a scraper (F-20). These
// patterns cover the inline Markdown a caption plausibly carries; block
// markup -- lists, quotes, headings -- would be strange in one and is left as
// written rather than half-removed.
var (
	mdImage    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLink     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdAutolink = regexp.MustCompile(`<((?:https?|mailto):[^>]*)>`)
	mdCode     = regexp.MustCompile("`+([^`]*)`+")
	// Raw HTML never reaches the page (R-4); it must not reach a reader's ears
	// through alt text either.
	mdTag = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	// RE2 has no backreferences, so each pair is its own pattern, longest first.
	mdEmphasis = []*regexp.Regexp{
		regexp.MustCompile(`\*\*\*([^*]+)\*\*\*`),
		regexp.MustCompile(`___([^_]+)___`),
		regexp.MustCompile(`\*\*([^*]+)\*\*`),
		regexp.MustCompile(`__([^_]+)__`),
		regexp.MustCompile(`\*([^*]+)\*`),
		regexp.MustCompile(`_([^_]+)_`),
		regexp.MustCompile(`~~([^~]+)~~`),
	}
	mdSpace = regexp.MustCompile(`[ \t\r\n]+`)
)

// markdownText reduces inline Markdown to the text it renders as: emphasis and
// code lose their markers, a link keeps its words and drops its target, and an
// escaped character becomes itself. Whitespace collapses, since alt text is one
// line however the caption was wrapped.
func markdownText(source string) string {
	out, escaped := hideEscapes(source)
	out = mdImage.ReplaceAllString(out, "$1")
	out = mdLink.ReplaceAllString(out, "$1")
	out = mdAutolink.ReplaceAllString(out, "$1")
	out = mdTag.ReplaceAllString(out, "")
	out = mdCode.ReplaceAllString(out, "$1")
	// Twice over the set: "**bold with *nested* inside**" needs the inner pair
	// gone before the outer one matches.
	for range 2 {
		for _, pattern := range mdEmphasis {
			out = pattern.ReplaceAllString(out, "$1")
		}
	}
	out = restoreEscapes(out, escaped)
	return strings.TrimSpace(mdSpace.ReplaceAllString(out, " "))
}

// hidden is the byte standing in for an escaped character while the patterns
// run. A caption that contains it already is not a caption.
const hidden = "\x00"

// hideEscapes takes \* out of the way of the emphasis patterns, which would
// otherwise read an escaped pair as emphasis, and returns what to put back.
func hideEscapes(source string) (string, []string) {
	var (
		out     strings.Builder
		escaped []string
	)
	out.Grow(len(source))
	for i := 0; i < len(source); i++ {
		if source[i] == '\\' && i+1 < len(source) &&
			strings.IndexByte("\\`*_{}[]()#+-.!<>|~", source[i+1]) >= 0 {
			escaped = append(escaped, string(source[i+1]))
			out.WriteString(hidden)
			i++
			continue
		}
		out.WriteByte(source[i])
	}
	return out.String(), escaped
}

func restoreEscapes(source string, escaped []string) string {
	for _, char := range escaped {
		source = strings.Replace(source, hidden, char, 1)
	}
	return source
}
