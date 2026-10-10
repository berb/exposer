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

func TestBuildVersionIsEmptyWhereThereIsNone(t *testing.T) {
	// D-10 prints this beside the credit, so "unknown" or "(devel)" must not
	// reach a footer: nothing is printed instead.
	if got := buildVersion(&debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}}, true); got != "v0.3.0" {
		t.Errorf("buildVersion = %q, want v0.3.0", got)
	}
	for _, info := range []*debug.BuildInfo{
		{Main: debug.Module{Version: "(devel)"}},
		{Main: debug.Module{Version: ""}},
		nil,
	} {
		if got := buildVersion(info, info != nil); got != "" {
			t.Errorf("an unstamped binary reports %q, want nothing", got)
		}
	}
}
