package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// These tests run the stages themselves, against the libraries in testdata/.
// They need the tools the stages shell out to, and skip — loudly, naming what
// is missing — where those are absent, so `go test ./...` stays useful on a
// machine that only has Go.

var update = flag.Bool("update", false, "rewrite the golden files from this run")

const (
	goodLibrary = "../../testdata/library"
	brokenRoot  = "../../testdata/broken"
	goldenDir   = "../../testdata/golden"
)

func requireTools(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			missingTool(t, "%s is not installed; this test drives it", name)
		}
	}
}

// missingTool skips a test that needs something this machine lacks -- unless
// EXPOSER_REQUIRE_TOOLS is set, as CI sets it. There a skip is a failure: an
// environment that silently lost a tool would otherwise report green while
// testing a fraction of what it claims to.
func missingTool(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("EXPOSER_REQUIRE_TOOLS") != "" {
		t.Fatalf(format+" (EXPOSER_REQUIRE_TOOLS is set)", args...)
	}
	t.Skipf(format, args...)
}

// buildIndex runs stage 1 over a library and returns the document it wrote.
func buildIndex(t *testing.T, library string) (Document, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "index.json")
	runIndex([]string{
		"--library", library,
		"--out", out,
		"--cache", filepath.Join(dir, "hashes.json"),
	})
	var doc Document
	read(t, out, &doc)
	return doc, out
}

func read(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
}

func TestIndexMatchesItsGolden(t *testing.T) {
	requireTools(t, "exiftool")

	_, out := buildIndex(t, goodLibrary)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// Two fields describe the machine rather than the library: the exiftool
	// version, and where the library happened to be mounted. Neither may decide
	// whether this test passes on someone else's computer.
	got = regexp.MustCompile(`"exiftool": "[^"]*"`).ReplaceAll(got, []byte(`"exiftool": "PINNED"`))
	got = regexp.MustCompile(`"library_root": "[^"]*"`).ReplaceAll(got, []byte(`"library_root": "LIBRARY"`))

	golden := filepath.Join(goldenDir, "index.json")
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v — run: go test -run Golden -update ./...", err)
	}
	if string(got) != string(want) {
		t.Errorf("index.json differs from its golden.\n%s", firstDifference(string(want), string(got)))
	}
}

// firstDifference reports the first line that differs, which is far easier to
// read than two thousand lines of JSON side by side.
func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return "line " + strconv.Itoa(i+1) + ":\n  want: " + w + "\n  got:  " + g
		}
	}
	return "(no line differs; the files differ in their trailing bytes)"
}

func TestIndexReadsTheLibraryItIsGiven(t *testing.T) {
	requireTools(t, "exiftool")

	doc, _ := buildIndex(t, goodLibrary)

	published := 0
	for _, photo := range doc.Photos {
		if photo.Published {
			published++
		}
	}
	// testdata/README.md states these; if they move, the README moves with them.
	if len(doc.Photos) != 11 || published != 9 {
		t.Errorf("%d photographs, %d published; want 11 and 9", len(doc.Photos), published)
	}
	if len(doc.Albums) != 2 || len(doc.Locations) != 2 || len(doc.Featured) != 3 {
		t.Errorf("%d albums, %d locations, %d featured; want 2, 2, 3",
			len(doc.Albums), len(doc.Locations), len(doc.Featured))
	}

	mappable := 0
	for _, place := range doc.Locations {
		if place.Lat != nil && place.Lon != nil {
			mappable++
		}
	}
	if mappable != 1 {
		t.Errorf("%d mappable locations, want 1 — a place may be named without being mapped (R-11)", mappable)
	}

	unlisted := 0
	for _, album := range doc.Albums {
		if album.Unlisted {
			unlisted++
		}
	}
	if unlisted != 1 {
		t.Errorf("%d unlisted albums, want 1 (F-18)", unlisted)
	}
}

func TestIndexFiltersWorkflowTags(t *testing.T) {
	requireTools(t, "exiftool")

	doc, _ := buildIndex(t, goodLibrary)
	for _, photo := range doc.Photos {
		for _, tag := range photo.Tags {
			if len(tag.Path) < 2 {
				t.Errorf("%s carries the flat tag %q; R-7 filters those", photo.Source, tag.Leaf)
			}
			if strings.EqualFold(tag.Path[0], "darktable") {
				t.Errorf("%s carries %v; R-7 filters the workflow namespace", photo.Source, tag.Path)
			}
		}
	}
}

func TestIndexCollapsesDuplicateOriginals(t *testing.T) {
	requireTools(t, "exiftool")

	doc, _ := buildIndex(t, goodLibrary)
	// The library holds the same bytes at two paths, curated identically.
	for _, photo := range doc.Photos {
		if photo.Source == "inbox/copy-of-lamp.jpg" {
			t.Error("the duplicate survived; R-12 collapses it into the first path")
		}
	}
	seen := map[string]string{}
	for _, photo := range doc.Photos {
		if previous, clash := seen[photo.File.SHA256]; clash {
			t.Errorf("%s and %s share a checksum", photo.Source, previous)
		}
		seen[photo.File.SHA256] = photo.Source
	}
}

func TestIndexRefusesTheBrokenLibraries(t *testing.T) {
	requireTools(t, "exiftool")

	// Each library trips exactly one failure. The substring is the part of the
	// message a person reads first: what is wrong, not where.
	for _, c := range []struct{ library, want string }{
		{"no-capture-date", "no capture date"},
		{"no-path", "no tags, album or location"},
		{"duplicate-conflict", "different metadata"},
		{"unknown-featured", "is not a photo in the library"},
	} {
		t.Run(c.library, func(t *testing.T) {
			dir := t.TempDir()
			message := catchFail(t, func() {
				runIndex([]string{
					"--library", filepath.Join(brokenRoot, c.library),
					"--out", filepath.Join(dir, "index.json"),
					"--cache", filepath.Join(dir, "hashes.json"),
				})
			})
			if message == "" {
				t.Fatalf("the build succeeded; %s must fail", c.library)
			}
			if !strings.Contains(message, c.want) {
				t.Errorf("failed with %q, want it to mention %q", message, c.want)
			}
		})
	}
}

// buildDerivatives runs stages 1 and 2 into one directory and returns it.
func buildDerivatives(t *testing.T, library string) (string, derivativeManifest) {
	t.Helper()
	dir := t.TempDir()
	runIndex([]string{
		"--library", library,
		"--out", filepath.Join(dir, "index.json"),
		"--cache", filepath.Join(dir, "hashes.json"),
	})
	// No --config: the library carries its own (R-18), which is the path every
	// build takes unless someone deliberately overrides it.
	runDerive([]string{
		"--index", filepath.Join(dir, "index.json"),
		"--library", library,
		"--cache", filepath.Join(dir, "cache"),
		"--manifest", filepath.Join(dir, "derivatives.json"),
	})
	var manifest derivativeManifest
	read(t, filepath.Join(dir, "derivatives.json"), &manifest)
	return dir, manifest
}

func TestDeriveRendersTheLadderAndNothingBeyondIt(t *testing.T) {
	requireTools(t, "exiftool", "magick")

	dir, manifest := buildDerivatives(t, goodLibrary)

	if len(manifest.Photos) != 9 {
		t.Fatalf("%d photographs rendered, want the 9 published ones", len(manifest.Photos))
	}

	for id, derivatives := range manifest.Photos {
		var scaled, squares int
		for _, d := range derivatives {
			if d.Square {
				squares++
			} else {
				scaled++
			}
			// F-5 renders both formats, and nothing else.
			if d.Format != "jpeg" && d.Format != "avif" {
				t.Errorf("%s: unexpected format %q", id, d.Format)
			}
			// ladder() never upscales: the fixtures are 900px on the long edge,
			// so 1600 and 2400 must not appear.
			if !d.Square && d.Width > 900 {
				t.Errorf("%s: a %dpx derivative from a 900px original", id, d.Width)
			}
			if d.Square && d.Width != d.Height {
				t.Errorf("%s: a square derivative measuring %dx%d", id, d.Width, d.Height)
			}
			if _, err := os.Stat(filepath.Join(dir, "cache", d.Cache)); err != nil {
				t.Errorf("%s: the manifest names a file that is not in the cache: %v", id, err)
			}
		}
		if scaled == 0 || squares == 0 {
			t.Errorf("%s: %d scaled and %d square derivatives; wanted both", id, scaled, squares)
		}
	}
}

func TestDeriveIsACacheNotAReRender(t *testing.T) {
	requireTools(t, "exiftool", "magick")

	dir, first := buildDerivatives(t, goodLibrary)

	// F-2: the same inputs must hit the cache rather than render again, and
	// must produce the identical manifest.
	runDerive([]string{
		"--index", filepath.Join(dir, "index.json"),
		"--library", goodLibrary,
		"--cache", filepath.Join(dir, "cache"),
		"--manifest", filepath.Join(dir, "second.json"),
	})
	var second derivativeManifest
	read(t, filepath.Join(dir, "second.json"), &second)

	if len(first.Photos) != len(second.Photos) {
		t.Fatalf("the second run produced %d photographs, the first %d",
			len(second.Photos), len(first.Photos))
	}
	for id, want := range first.Photos {
		got, ok := second.Photos[id]
		if !ok {
			t.Errorf("%s vanished on the second run", id)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("%s: %d derivatives, then %d", id, len(want), len(got))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: derivative %d changed between runs:\n  %+v\n  %+v",
					id, i, want[i], got[i])
			}
		}
	}
}

func TestDeriveRendersNothingForAnUnpublishedPhotograph(t *testing.T) {
	requireTools(t, "exiftool", "magick")

	dir, manifest := buildDerivatives(t, goodLibrary)
	var doc Document
	read(t, filepath.Join(dir, "index.json"), &doc)

	for _, photo := range doc.Photos {
		_, rendered := manifest.Photos[photo.ID]
		if photo.Published && !rendered {
			t.Errorf("%s is published but has no derivatives", photo.Source)
		}
		// R-5: a photograph below the gate costs nothing to keep in the library
		// and must cost nothing to build.
		if !photo.Published && rendered {
			t.Errorf("%s is below the gate but was rendered", photo.Source)
		}
	}
}

func TestFixtureLibraryCarriesItsOwnConfig(t *testing.T) {
	// R-18. The fixture's configuration was once ignored by an unanchored
	// .gitignore rule: it existed on the machine that wrote it and nowhere
	// else, so every clone built the fixtures on defaults without saying so.
	// configFor falls back to defaults quietly, which is why this asks for the
	// file itself and then for a value only the file sets.
	path := filepath.Join(goodLibrary, libraryData, configFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s is missing: %v -- is it ignored?", path, err)
	}
	if got := configFor(goodLibrary, "").Title; got != "Test Library" {
		t.Errorf("the fixture builds as %q, not with its own configuration", got)
	}
}

func TestJPEGLibraryBuildsWithoutDarktable(t *testing.T) {
	// F-4: darktable develops RAW originals, and the fixtures hold none. A PATH
	// with exiftool and ImageMagick but no darktable-cli is exactly a machine
	// that only ever shoots JPEG, and derive must not ask it for darktable.
	requireTools(t, "exiftool", "magick", "perl")
	bin := t.TempDir()
	for _, tool := range []string{"exiftool", "magick", "perl"} {
		path, _ := exec.LookPath(tool)
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	if message := catchFail(t, func() { buildDerivatives(t, goodLibrary) }); message != "" {
		t.Fatalf("derive failed without darktable on a JPEG library: %s", message)
	}
}

func TestConfigRefusesKeysItDoesNotKnow(t *testing.T) {
	// R-18. An unknown key used to be ignored, so a typo built the site on a
	// default without a word.
	write := func(body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), configFileName)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	if message := catchFail(t, func() { loadConfig(write("base_ulr: \"/photos/\"\n")) }); !strings.Contains(message, "base_ulr") {
		t.Errorf("a misspelt key gave %q, want it named", message)
	}
	// A setting from an earlier release is told its new name.
	if message := catchFail(t, func() { loadConfig(write("footer_note: \"© me\"\n")) }); !strings.Contains(message, "renamed to footer_line") {
		t.Errorf("footer_note gave %q, want the rename", message)
	}
	// An empty file is still the defaults, not an error.
	if message := catchFail(t, func() { loadConfig(write("")) }); message != "" {
		t.Errorf("an empty configuration failed: %s", message)
	}
}
