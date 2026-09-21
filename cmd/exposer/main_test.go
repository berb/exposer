package main

import (
	"strings"
	"testing"
)

func TestLogoFitsATerminal(t *testing.T) {
	// The usage text opens with the logo, so it has to survive the narrowest
	// terminal anyone still uses and a copy-paste into an issue.
	for i, line := range strings.Split(strings.TrimRight(logo, "\n"), "\n") {
		if len(line) > 80 {
			t.Errorf("logo line %d is %d columns wide", i+1, len(line))
		}
		if line != strings.TrimRight(line, " ") {
			t.Errorf("logo line %d has trailing spaces", i+1)
		}
		for _, r := range line {
			if r > 127 {
				t.Errorf("logo line %d is not plain ASCII: %q", i+1, r)
				break
			}
		}
	}
}
