package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// versionLine is what `exposer --version` prints: the release, and the Hugo it
// renders with, since both decide the bytes of a build (F-3, B-8).
//
// The version is the one Go stamps into the binary, so there is nothing to
// keep in step by hand: `go install …@v0.1.0` reports v0.1.0, and a build from
// a checkout reports a pseudo-version naming its commit, with +dirty when the
// tree had uncommitted changes. Only a binary built without module information
// reports "unknown".
func versionLine(info *debug.BuildInfo, ok bool) string {
	version := "unknown"
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return fmt.Sprintf("exposer %s (hugo %s, %s, %s/%s)",
		version, hugoVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func runVersion() {
	info, ok := debug.ReadBuildInfo()
	fmt.Println(versionLine(info, ok))
}
