// Package exposer carries the files the command needs besides the library: the
// theme and the index schema. They are compiled into the binary (B-8), so an
// installed exposer builds a site from anywhere, and a binary can never run
// against templates from a different version of itself.
package exposer

import "embed"

// Files holds site-gen/ and schema/. The all: prefix is load-bearing: without
// it, go:embed skips every name starting with "_" or ".", which would silently
// drop site-gen/layouts/_partials/ and fail every page that renders a grid.
//
//go:embed all:site-gen schema
var Files embed.FS
