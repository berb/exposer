package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	fs.Parse(args)
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

	runIndex(withConfig("--library", library, "--out", index,
		"--cache", filepath.Join(*target, "cache", "hashes.json")))
	runDerive(withConfig("--index", index, "--library", library,
		"--cache", derivatives, "--manifest", manifest))
	contentArgs := withConfig("--index", index, "--manifest", manifest,
		"--library", library, "--out", project)
	if *theme != "" {
		contentArgs = append(contentArgs, "--theme", *theme)
	}
	runContent(contentArgs)

	render := exec.Command(hugoBinary, "--source", project, "--destination", "public", "--quiet")
	render.Stdout, render.Stderr = os.Stdout, os.Stderr
	if err := render.Run(); err != nil {
		fail("hugo failed: %v", err)
	}

	runAssemble([]string{"--public", public, "--manifest", manifest,
		"--cache", derivatives, "--index", index, "--out", site})
}
