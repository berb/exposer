package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// How much a run says. Errors are never affected: fail prints and exits
// whatever the level, since a build that fails quietly is the one thing worse
// than a noisy one.
type level int

const (
	quiet   level = -1 // errors only
	normal  level = 0  // one summary line per stage, and notices
	verbose level = 1  // plus a line per photograph that is worth one
)

var (
	outputLevel = normal
	outputMu    sync.Mutex
	// Where summaries and everything else go; variables so tests can read them.
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// addOutputFlags gives a command -v/--verbose and -q/--quiet. They return the
// flags' values so applyOutputFlags can read them after parsing.
func addOutputFlags(fs *flag.FlagSet) (v, q *bool) {
	v = new(bool)
	q = new(bool)
	fs.BoolVar(v, "v", false, "short for -verbose")
	fs.BoolVar(v, "verbose", false, "say why photographs were left out, which were rendered, and what each stage took")
	fs.BoolVar(q, "q", false, "short for -quiet")
	fs.BoolVar(q, "quiet", false, "print nothing but errors")
	return v, q
}

// applyOutputFlags sets the level from a command's flags. A stage run by
// `exposer build` inherits the build's level; its own flags only ever raise
// or lower it when given.
func applyOutputFlags(v, q *bool) {
	switch {
	case *v && *q:
		fail("-v and -q contradict each other; choose one")
	case *v:
		outputLevel = verbose
	case *q:
		outputLevel = quiet
	}
}

// report prints a stage's summary line to stdout.
func report(format string, args ...any) {
	if outputLevel < normal {
		return
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	fmt.Fprintf(stdout, format+"\n", args...)
}

// notice prints something a reader should know but that is not an error --
// "no configuration, using the defaults" -- to stderr.
func notice(format string, args ...any) {
	if outputLevel < normal {
		return
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	fmt.Fprintf(stderr, format+"\n", args...)
}

// detail prints a verbose line to stderr, so stdout keeps only the summaries
// and `exposer build -v > build.log` still separates the two.
func detail(format string, args ...any) {
	if outputLevel < verbose {
		return
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	fmt.Fprintf(stderr, "  "+format+"\n", args...)
}

// shown shortens a path for a summary line: relative to the working
// directory when it lies beneath it, otherwise as given.
func shown(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}
