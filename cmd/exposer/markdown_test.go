package main

import "testing"

func TestMarkdownTextKeepsTheWordsAndDropsTheSyntax(t *testing.T) {
	// D-9: this is what a screen reader says, so no reader should hear an
	// asterisk or a URL that was written as a link.
	cases := map[string]string{
		"The first light over the *container cranes*.": "The first light over the container cranes.",
		"Seen from [the quay](https://example.com/q)":  "Seen from the quay",
		"**Bold**, _italic_, ***both*** and ~~gone~~":  "Bold, italic, both and gone",
		"**bold with *nested* inside**":                "bold with nested inside",
		"`magick -resize` on the original":             "magick -resize on the original",
		"![a crane](/photos/img/a.jpg) beside it":      "a crane beside it",
		"Write <https://example.com/> plainly":         "Write https://example.com/ plainly",
		"A literal \\*asterisk\\* survives":            "A literal *asterisk* survives",
		"Two lines\nbecome one":                        "Two lines become one",
		"  padded  ":                                   "padded",
		"Nothing to do here":                           "Nothing to do here",
		"":                                             "",
		// R-4 drops raw HTML from the page; alt text loses it too, keeping
		// whatever text stood between the tags.
		"Nothing moved. <script>alert(1)</script>": "Nothing moved. alert(1)",
		"A <em>tagged</em> word":                   "A tagged word",
		"5 < 7 and 9 > 2":                          "5 < 7 and 9 > 2",
		// A lone marker is not a pair and is left alone rather than eaten.
		"5 * 3 = 15": "5 * 3 = 15",
	}
	for source, want := range cases {
		if got := markdownText(source); got != want {
			t.Errorf("markdownText(%q) = %q, want %q", source, got, want)
		}
	}
}
