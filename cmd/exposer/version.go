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
	version := buildVersion(info, ok)
	if version == "" {
		version = "unknown"
	}
	return fmt.Sprintf("exposer %s (hugo %s, %s, %s/%s)",
		version, hugoVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// buildVersion is the stamped version, or "" where Go has none to give -- a
// binary built without module information. The footer (D-10) prints it beside
// the credit, and prints the credit alone when there is nothing to print.
func buildVersion(info *debug.BuildInfo, ok bool) string {
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return ""
}

// exposerVersion is buildVersion for the running binary.
func exposerVersion() string {
	info, ok := debug.ReadBuildInfo()
	return buildVersion(info, ok)
}

func runVersion() {
	info, ok := debug.ReadBuildInfo()
	fmt.Println(versionLine(info, ok))
}
