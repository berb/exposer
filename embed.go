// Package exposer carries the files the command needs besides the library: the
// theme, the index schema and the configuration template. They are compiled into the binary (B-8), so an
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

// ExampleConfig is exposer.example.yaml: every setting at its default, with
// what each one does. `exposer init` writes it into a library, and it is the
// same file the repository shows, so the two cannot drift apart (R-18).
//
//go:embed exposer.example.yaml
var ExampleConfig []byte
