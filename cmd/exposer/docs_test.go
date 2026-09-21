package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The code cites requirement IDs by number -- "R-8", "F-15", "§12 criterion 4".
// That only works while the numbers mean something, and a citation rots
// silently: nothing fails, the comment simply starts pointing at nothing. These
// two tests are the thing that fails instead.

var (
	citation = regexp.MustCompile(`\b([RFDNB]-[0-9]+[a-z]?)\b`)
	defined  = regexp.MustCompile(`(?m)^\*\*([RFDNB]-[0-9]+[a-z]?)`)
	section  = regexp.MustCompile(`§([0-9]+)`)
	heading  = regexp.MustCompile(`(?m)^## ([0-9]+)\.`)
)

func TestEveryCitedRequirementExists(t *testing.T) {
	known := matchesIn(t, defined, spec(t))
	// Withdrawn requirements keep their number and their reasoning, so a
	// citation of one is still a citation of something the spec says.
	for id := range matchesIn(t, regexp.MustCompile(`(?m)^\*\*([RFDNB]-[0-9]+[a-z]?) — withdrawn`), spec(t)) {
		known[id] = true
	}

	for file, ids := range citationsInTree(t) {
		for id := range ids {
			if !known[id] {
				t.Errorf("%s cites %s, which docs/spec.md does not define", file, id)
			}
		}
	}
}

func TestEveryCitedSectionExists(t *testing.T) {
	known := matchesIn(t, heading, spec(t))
	for file, body := range treeFiles(t) {
		for _, m := range section.FindAllStringSubmatch(body, -1) {
			if !known[m[1]] {
				t.Errorf("%s cites §%s, which docs/spec.md does not have", file, m[1])
			}
		}
	}
}

func spec(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../docs/spec.md")
	if err != nil {
		t.Fatalf("the specification is what the citations point at: %v", err)
	}
	return string(raw)
}

func matchesIn(t *testing.T, re *regexp.Regexp, body string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatalf("%s matched nothing in the specification", re)
	}
	return out
}

func citationsInTree(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for file, body := range treeFiles(t) {
		ids := map[string]bool{}
		for _, m := range citation.FindAllStringSubmatch(body, -1) {
			ids[m[1]] = true
		}
		if len(ids) > 0 {
			out[file] = ids
		}
	}
	return out
}

// treeFiles reads everything a citation could hide in: the pipeline, the
// templates, the fixtures and the documentation, minus the specification
// itself, which is where the definitions live, and minus build output.
func treeFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, "../../"))
		if entry.IsDir() {
			if strings.HasPrefix(rel, "target") || rel == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(rel) {
		case ".go", ".md", ".html", ".sh", ".yml", ".yaml", ".json":
		default:
			if filepath.Base(rel) != "Makefile" {
				return nil
			}
		}
		if rel == "docs/spec.md" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	return out
}
