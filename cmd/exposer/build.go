package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// renderSite runs Hugo over the content stage's project. Hugo's output is
// captured rather than passed through: B-9 keeps a build to one line per
// stage, and Hugo's banner and statistics table are not that line. They are
// printed under -v, and in full when it fails -- which is why --quiet is not
// used, since that silences Hugo's errors too and leaves a build with nothing
// but "exit status 1" to go on.
func renderSite(hugoBinary, project string) {
	render := exec.Command(hugoBinary, "--source", project, "--destination", "public")
	var log bytes.Buffer
	render.Stdout, render.Stderr = &log, &log
	err := render.Run()
	output := strings.TrimRight(log.String(), "\n")
	if err != nil {
		fail("hugo failed: %v\n%s", err, output)
	}
	for _, line := range strings.Split(output, "\n") {
		if line != "" {
			detail("hugo: %s", line)
		}
	}
}

// runBuild is the whole pipeline in one command (B-8): index, derive, content,
// Hugo, assemble. It chains the same stage functions the subcommands run, with
// the same flags, so a build and a stage-by-stage debugging session cannot
// disagree about what a stage does.
//
// Everything lands under --target: the artifact at <target>/site, which is the
// only part that ever deploys, and the index, manifests, caches and Hugo
// project beside it (B-3).
func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	target := fs.String("target", "target", "directory for the artifact (<target>/site) and its caches")
	configPath := fs.String("config", "", "generator config (default: <library>/_data/exposer.yaml)")
	theme := fs.String("theme", "", "theme directory to use instead of the built-in one")
	hugo := fs.String("hugo", "", "Hugo binary to use instead of the pinned download")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: exposer build [flags] <library>\n\n")
		fs.PrintDefaults()
	}
	// The library usually comes first, as people type it, but the flag package
	// stops at the first positional argument. So parse the flags on whichever
	// side of it they were written, and require exactly one library.
	var library string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		library, args = args[0], args[1:]
	}
	v, q := addOutputFlags(fs)
	fs.Parse(args)
	applyOutputFlags(v, q)
	if library == "" && fs.NArg() == 1 {
		library = fs.Arg(0)
	} else if fs.NArg() != 0 || library == "" {
		fs.Usage()
		os.Exit(2)
	}

	if info, err := os.Stat(library); err != nil || !info.IsDir() {
		fail("%s is not a library directory", library)
	}
	hugoBinary := findHugo(*hugo)
	detail("hugo %s  %s", hugoVersion, shown(hugoBinary))

	var (
		index       = filepath.Join(*target, "index.json")
		manifest    = filepath.Join(*target, "derivatives.json")
		derivatives = filepath.Join(*target, "cache", "derivatives")
		project     = filepath.Join(*target, "hugo")
		public      = filepath.Join(project, "public")
		site        = filepath.Join(*target, "site")
	)
	withConfig := func(args ...string) []string {
		if *configPath != "" {
			args = append(args, "--config", *configPath)
		}
		return args
	}

	timings := []string{}
	timed := func(name string, stage func()) {
		started := time.Now()
		stage()
		timings = append(timings, fmt.Sprintf("%s %s", name, time.Since(started).Round(10*time.Millisecond)))
	}
	defer func() { detail("time: %s", strings.Join(timings, " · ")) }()

	timed("index", func() {
		runIndex(withConfig("--library", library, "--out", index,
			"--cache", filepath.Join(*target, "cache", "hashes.json")))
	})
	timed("derive", func() {
		runDerive(withConfig("--index", index, "--library", library,
			"--cache", derivatives, "--manifest", manifest))
	})
	contentArgs := withConfig("--index", index, "--manifest", manifest,
		"--library", library, "--out", project)
	if *theme != "" {
		contentArgs = append(contentArgs, "--theme", *theme)
	}
	timed("content", func() { runContent(contentArgs) })

	timed("hugo", func() { renderSite(hugoBinary, project) })

	timed("assemble", func() {
		runAssemble([]string{"--public", public, "--manifest", manifest,
			"--cache", derivatives, "--index", index, "--out", site})
	})
}
