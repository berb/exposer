package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionLineNamesTheReleaseAndTheRenderer(t *testing.T) {
	// F-3 and B-8: a bug report about output needs both versions, since
	// either one changes the bytes.
	release := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}
	got := versionLine(release, true)
	for _, want := range []string{"exposer v0.1.0", "hugo " + hugoVersion} {
		if !strings.Contains(got, want) {
			t.Errorf("versionLine = %q, want it to contain %q", got, want)
		}
	}

	// A binary Go could not stamp says so, rather than claiming "(devel)".
	for _, info := range []*debug.BuildInfo{{Main: debug.Module{Version: "(devel)"}}, nil} {
		if got := versionLine(info, info != nil); !strings.HasPrefix(got, "exposer unknown ") {
			t.Errorf("an unstamped binary reports %q", got)
		}
	}
}
