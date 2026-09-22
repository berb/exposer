package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssemblyRefusesADerivativeNotNamedForItsContents(t *testing.T) {
	// B-10: a host may cache these files forever, so a name that does not
	// match its bytes is a build failure, never a warning.
	root := t.TempDir()
	const id = "e7a82ee2493d05a7"
	content := []byte("an AVIF, as far as this check cares")
	digest := sha256.Sum256(content)
	sum := hex.EncodeToString(digest[:])[:publicHashLen]
	place := func(rel string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	good := "photos/img/e7/" + id + "/" + id + "-800." + sum + ".avif"
	place(good)
	if misnamed := checkDerivativeNames(root); len(misnamed) != 0 {
		t.Fatalf("a correctly named derivative was refused: %v", misnamed)
	}

	bad := map[string]string{
		"photos/img/e7/" + id + "/" + id + "-400.00000000.avif":          "contents hash to",
		"photos/img/ff/" + id + "/" + id + "-200." + sum + ".avif":       "disagree",
		"photos/img/e7/" + id + "/e7ffffffffffffff-200." + sum + ".avif": "disagree",
		"photos/img/e7/" + id + "/800.avif":                              "not <id[:2]>",
	}
	for rel := range bad {
		place(rel)
	}
	misnamed := checkDerivativeNames(root)
	if len(misnamed) != len(bad) {
		t.Fatalf("refused %d files, want %d: %v", len(misnamed), len(bad), misnamed)
	}
	for _, line := range misnamed {
		rel, _, _ := strings.Cut(line, " ")
		reason, ok := bad[rel]
		if !ok {
			t.Errorf("refused %q, which was not planted", line)
		} else if !strings.Contains(line, reason) {
			t.Errorf("refused %q, want a reason containing %q", line, reason)
		}
	}
}

func TestAssemblyAcceptsASiteWithoutImages(t *testing.T) {
	// A library with nothing published yet builds; no photos/img/ is no error.
	if misnamed := checkDerivativeNames(t.TempDir()); len(misnamed) != 0 {
		t.Errorf("an empty site was refused: %v", misnamed)
	}
}
