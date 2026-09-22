package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captured runs body at the given level and returns what it printed to stdout
// and stderr, restoring everything the output helper keeps between runs.
func captured(t *testing.T, at level, body func()) (out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	savedLevel, savedOut, savedErr, savedSeen := outputLevel, stdout, stderr, announced
	outputLevel, stdout, stderr, announced = at, &o, &e, map[string]bool{}
	defer func() { outputLevel, stdout, stderr, announced = savedLevel, savedOut, savedErr, savedSeen }()
	body()
	return o.String(), e.String()
}

// libraryWithoutConfig copies the fixture library minus its configuration, so
// a build has to say it is using the defaults.
func libraryWithoutConfig(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "library")
	err := filepath.Walk(goodLibrary, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(goodLibrary, path)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if rel == filepath.Join(libraryData, configFileName) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestVerboseSaysWhyAPhotoIsNotPublished(t *testing.T) {
	// The question a photographer asks after a build is "where is my photo?"
	// The index knows; -v says it, per photograph, with the reason.
	requireTools(t, "exiftool")
	_, errs := captured(t, verbose, func() { buildIndex(t, goodLibrary) })
	for _, want := range []string{
		"2022/05/rejected.jpg  not published: rating 3 is below 4",
		"2022/05/untouched.jpg  not published: no sidecar",
		"inbox/copy-of-lamp.jpg  same original as 2023/11/street-lamp.jpg",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("verbose output lacks %q:\n%s", want, errs)
		}
	}
}

func TestOutputLevels(t *testing.T) {
	requireTools(t, "exiftool", "magick")
	hugo := hugoBinary(t)
	library := libraryWithoutConfig(t)
	target := t.TempDir()
	build := func(at level) (string, string) {
		return captured(t, at, func() { runBuild([]string{library, "--target", target, "--hugo", hugo}) })
	}

	// Verbose, on the first (cold) build: every photograph is rendered and
	// listed as such, with the tools and the time; stdout is unchanged.
	vout, verrs := build(verbose)
	if n := strings.Count(vout, "\n"); n != 4 || strings.Contains(vout, "not published") {
		t.Errorf("-v changed what goes to stdout (%d lines):\n%s", n, vout)
	}
	for _, want := range []string{"not published", "2024/03/harbour-dawn.jpg  rendered", "hugo " + hugoVersion, "time: index"} {
		if !strings.Contains(verrs, want) {
			t.Errorf("-v output lacks %q:\n%s", want, verrs)
		}
	}

	// Normal, now warm: one summary per stage on stdout, the missing
	// configuration mentioned once -- every stage loads it, so it used to be
	// said three times -- and no per-photograph detail.
	out, errs := build(normal)
	if n := strings.Count(out, "\n"); n != 4 {
		t.Errorf("a build printed %d summary lines, want 4 (index, derive, content, assemble):\n%s", n, out)
	}
	if n := strings.Count(errs, "building with the defaults"); n != 1 {
		t.Errorf("the defaults notice appeared %d times, want once:\n%s", n, errs)
	}
	if strings.Contains(out+errs, "not published") {
		t.Errorf("per-photo detail printed without -v:\n%s%s", out, errs)
	}

	// Verbose again, warm: nothing was rendered, and cached photographs are
	// not listed one by one -- on a real library that would be hundreds of
	// lines saying nothing the summary does not.
	if _, warm := build(verbose); strings.Contains(warm, "  rendered") || strings.Contains(warm, "  cached") {
		t.Errorf("a warm -v build listed photographs it did not render:\n%s", warm)
	}

	// Quiet: a successful build says nothing at all.
	if qout, qerrs := build(quiet); qout != "" || qerrs != "" {
		t.Errorf("-q printed:\n%s%s", qout, qerrs)
	}
}

func TestVerboseAndQuietContradict(t *testing.T) {
	v, q := true, true
	if message := catchFail(t, func() { applyOutputFlags(&v, &q) }); !strings.Contains(message, "contradict") {
		t.Errorf("-v -q gave %q", message)
	}
}

func TestShortVersion(t *testing.T) {
	for banner, want := range map[string]string{
		"Version: ImageMagick 7.1.2-18 Q16 x86_64 23822 https://imagemagick.org": "7.1.2-18",
		"13.50":                   "13.50",
		"this is darktable 5.2.1": "5.2.1",
	} {
		if got := shortVersion(banner); got != want {
			t.Errorf("shortVersion(%q) = %q, want %q", banner, got, want)
		}
	}
}
