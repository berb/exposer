package main

import (
	"io/fs"
	"os"

	"github.com/berb/exposer"
)

// themeFS is the theme a build renders with: the one compiled into the binary
// (B-8), or a directory given with --theme for anyone who wants to change the
// look without rebuilding exposer. Either way the result is rooted at the
// theme itself, so layouts/ and static/ sit at its top.
func themeFS(dir string) fs.FS {
	if dir != "" {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fail("--theme %s is not a directory", dir)
		}
		return os.DirFS(dir)
	}
	sub, err := fs.Sub(exposer.Files, "site-gen")
	if err != nil {
		fail("the built-in theme is missing from this binary: %v", err)
	}
	return sub
}

// builtinSchema is the index schema compiled into the binary, which is what
// `exposer validate` checks against unless --schema names another file.
const builtinSchema = "schema/index.schema.json"
