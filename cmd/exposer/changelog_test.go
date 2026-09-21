package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangelogIsWrittenForUsers(t *testing.T) {
	// The changelog becomes the release notes, so it is read by people using
	// exposer, not by people working on it: the specification's IDs, which
	// the rest of the tree must cite correctly, must not appear here at all.
	raw, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if m := citation.FindString(line); m != "" {
			t.Errorf("CHANGELOG.md:%d cites %s; say what changed instead", i+1, m)
		}
		if strings.Contains(line, "§") {
			t.Errorf("CHANGELOG.md:%d cites a section of the specification", i+1)
		}
	}
}

func TestReleaseNotesComeFromTheChangelog(t *testing.T) {
	// .github/release-notes.sh is what the release workflow publishes, and
	// what stops a tag nobody described. A bug in it shows only on release.
	if _, err := exec.LookPath("bash"); err != nil {
		missingTool(t, "bash is not installed; the release notes script needs it")
	}
	script, err := filepath.Abs("../../.github/release-notes.sh")
	if err != nil {
		t.Fatal(err)
	}
	changelog := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(changelog, []byte(`# Changelog

## [Unreleased]

- pending

## [1.2.0] - 2026-01-02

### Added

- the new thing

## [1.1.0] - 2026-01-01

- the old thing

[Unreleased]: https://example.com/compare/v1.2.0...HEAD
[1.2.0]: https://example.com/compare/v1.1.0...v1.2.0
`), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := func(tag string) (string, error) {
		out, err := exec.Command("bash", script, tag, changelog).Output()
		return string(out), err
	}

	if got, err := notes("v1.2.0"); err != nil || got != "### Added\n\n- the new thing\n" {
		t.Errorf("v1.2.0 gave %q (%v), want its section alone, trimmed", got, err)
	}
	// The last section ends at the link references, not at the end of file.
	if got, err := notes("v1.1.0"); err != nil || got != "- the old thing\n" {
		t.Errorf("v1.1.0 gave %q (%v), want its section without the links", got, err)
	}
	// A version nobody wrote up fails, which is what stops the release.
	if _, err := notes("v1.3.0"); err == nil {
		t.Errorf("a tag with no section succeeded")
	}
	// "1.2.0" must not match "1.200" or "1x2x0": the dots are literal.
	if _, err := notes("v1x2x0"); err == nil {
		t.Errorf("a version with other characters in place of the dots matched")
	}
}
