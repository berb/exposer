package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/berb/exposer"
)

// runInit writes the configuration template into a library (R-18): every
// setting at its default, with what each one does, so the file changes nothing
// until someone edits it. It is the one command that writes into a library,
// and it writes exactly one file there — never an original or a sidecar (R-1).
func runInit(args []string) {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: exposer init <library>")
		os.Exit(2)
	}
	library := args[0]

	// A mistyped path should fail, not quietly start an empty library.
	if info, err := os.Stat(library); err != nil || !info.IsDir() {
		fail("%s is not a directory; init writes into an existing library", library)
	}

	dir := filepath.Join(library, libraryData)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail("cannot create %s: %v", dir, err)
	}
	path := filepath.Join(dir, configFileName)

	// O_EXCL, not a check followed by a write: an existing configuration is the
	// photographer's, and nothing may replace it, not even a race.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		fail("%s already exists; edit it rather than starting over", path)
	}
	if err != nil {
		fail("cannot write %s: %v", path, err)
	}
	if _, err := file.Write(exposer.ExampleConfig); err != nil {
		file.Close()
		fail("cannot write %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		fail("cannot write %s: %v", path, err)
	}

	fmt.Printf("wrote %s — every setting at its default; edit base_url and title, then: exposer build %s\n",
		path, library)
}
