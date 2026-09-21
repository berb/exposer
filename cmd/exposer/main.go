// exposer — the build pipeline for a static photography site.
//
// `exposer build` runs the whole pipeline (F-1, B-8); its stages are also
// subcommands, so each can be run on its own while developing.
package main

import (
	"fmt"
	"os"
)

// logo heads the usage text. Plain ASCII in the figlet "small" style, so it
// survives any terminal and any font.
const logo = `  ___  __ __  _ __   ___   ___  ___   _ _
 / -_) \ \ / | '_ \ / _ \ (_-< / -_) | '_|
 \___| /_\_\ | .__/ \___/ /__/ \___| |_|
             |_|
`

const usage = logo + `
exposer <command> [flags]

  build     the whole pipeline: exposer build <library>, artifact in target/site
  serve     serve the assembled artifact for local preview (B-7)

  The stages, for running one at a time:
  index     read the library, write target/index.json         (stage 1)
  derive    render derivatives for every published photo      (stage 2)
  content   write Hugo content and data from the index        (stage 3)
  assemble  hardlink Hugo output and derivatives into the artifact
  validate  check a document against the index schema

Run "exposer <command> -h" for the flags of one command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "build":
		runBuild(args)
	case "index":
		runIndex(args)
	case "derive":
		runDerive(args)
	case "content":
		runContent(args)
	case "assemble":
		runAssemble(args)
	case "validate":
		runValidate(args)
	case "serve":
		runServe(args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
