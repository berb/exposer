package main

import (
	"io/fs"
	"testing"

	"github.com/berb/exposer"
)

func TestEmbeddedThemeKeepsItsPartials(t *testing.T) {
	// B-8. go:embed drops names starting with "_" unless the pattern says all:,
	// and nothing about the build would say so -- the binary compiles, and only
	// the Hugo render fails, far from the cause.
	for _, name := range []string{
		"site-gen/layouts/baseof.html",
		"site-gen/layouts/_partials/squares.html",
		"site-gen/assets/photo.js",
		"schema/index.schema.json",
	} {
		if _, err := fs.Stat(exposer.Files, name); err != nil {
			t.Errorf("%s is not embedded: %v", name, err)
		}
	}
}
