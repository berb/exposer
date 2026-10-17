package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// catchFail runs body with fail() replaced, so a test can reach the paths that
// end the build. It returns what fail was called with, or "" if it was not.
// The return is named on purpose: fail panics to stop the code under test where
// the real one would have exited, so the value has to survive the unwind.
func catchFail(t *testing.T, body func()) (message string) {
	t.Helper()
	original := fail
	fail = func(format string, args ...any) {
		message = fmt.Sprintf(format, args...)
		panic(sentinel{})
	}
	defer func() {
		fail = original
		if r := recover(); r != nil {
			if _, ours := r.(sentinel); !ours {
				panic(r)
			}
		}
	}()
	body()
	return message
}

type sentinel struct{}

func TestSlugify(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Harbour", "harbour"},
		{"Ulm & Neu-Ulm", "ulm-neu-ulm"},
		{"OLYMPUS M.12-40mm F2.8", "olympus-m-12-40mm-f2-8"},
		{"Black/White", "black-white"},
		{"  spaced  out  ", "spaced-out"},
		{"Ärger", "arger"},
	} {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugifyIsStableUnderRepetition(t *testing.T) {
	// A slug fed back in must not change: URLs are built from these (F-3).
	for _, in := range []string{"Places|Harbour", "TEST 7-14/F4.0", "Norway|Bergen"} {
		once := slugify(in)
		if twice := slugify(once); twice != once {
			t.Errorf("slugify(%q) = %q, but slugify(%q) = %q", in, once, once, twice)
		}
	}
}

func TestClassifyTagsSplitsTheNamespaces(t *testing.T) {
	albums, locations, display := classifyTags([]string{
		"Album|Harbour",
		"Location|Norway|Bergen",
		"Subject|Street",
		"darktable|exported", // R-7: workflow, never displayed
		"todo",               // R-7: un-namespaced, never displayed
		"Album|Harbour",      // repeated: deduplicated
	})
	if want := []string{"Harbour"}; !reflect.DeepEqual(albums, want) {
		t.Errorf("albums = %v, want %v", albums, want)
	}
	if want := [][]string{{"Norway", "Bergen"}}; !reflect.DeepEqual(locations, want) {
		t.Errorf("locations = %v, want %v", locations, want)
	}
	if want := [][]string{{"Subject", "Street"}}; !reflect.DeepEqual(display, want) {
		t.Errorf("display = %v, want %v", display, want)
	}
}

func TestClassifyTagsKeepsOutputSorted(t *testing.T) {
	// The index must not depend on the order exiftool happened to return (F-3).
	_, _, display := classifyTags([]string{"Subject|Zebra", "Subject|Apple", "Places|Harbour"})
	want := [][]string{{"Places", "Harbour"}, {"Subject", "Apple"}, {"Subject", "Zebra"}}
	if !reflect.DeepEqual(display, want) {
		t.Errorf("display = %v, want %v", display, want)
	}
}

func TestWithGearTagsAddsKindAndDeduplicates(t *testing.T) {
	model, lens := "TESTCAM A1", "TEST 12-40mm F2.8"
	display := withGearTags(nil,
		gearKind{gearCamera, &model},
		gearKind{gearLens, &lens},
		gearKind{gearCamera, &model}) // same body twice
	want := [][]string{
		{"Gear", "Camera", "TESTCAM A1"},
		{"Gear", "Lens", "TEST 12-40mm F2.8"},
	}
	if !reflect.DeepEqual(display, want) {
		t.Errorf("display = %v, want %v", display, want)
	}
}

func TestWithGearTagsIgnoresEmptyValues(t *testing.T) {
	// A scanned print has no camera in EXIF, and must not grow a blank tag.
	blank := "   "
	if got := withGearTags(nil, gearKind{gearCamera, nil}, gearKind{gearLens, &blank}); len(got) != 0 {
		t.Errorf("display = %v, want none", got)
	}
}

func TestValidateContract(t *testing.T) {
	taken := "2024-03-04T07:12:00"
	base := func() Photo {
		return Photo{CapturedAt: &taken, curatedTags: 1}
	}
	if problems := validateContract(base()); len(problems) != 0 {
		t.Errorf("a complete photo reported %v", problems)
	}

	undated := base()
	undated.CapturedAt = nil
	if got := validateContract(undated); len(got) != 1 || got[0] != "no capture date" {
		t.Errorf("undated photo reported %v", got)
	}

	orphan := base()
	orphan.curatedTags = 0
	if got := validateContract(orphan); len(got) != 1 || got[0] != "no tags, album or location" {
		t.Errorf("orphan reported %v", got)
	}

	// R-8 counts gear tags for nothing: nearly every photograph has a camera,
	// so letting them answer would make the check vacuous.
	gearOnly := orphan
	gearOnly.Tags = []Tag{{Slug: "gear-camera-testcam-a1", Path: []string{"Gear", "Camera", "TESTCAM A1"}}}
	if got := validateContract(gearOnly); len(got) != 1 {
		t.Errorf("gear-only photo reported %v, want the same one problem", got)
	}

	// An album alone is a way to be found, and so is a place.
	byAlbum := orphan
	byAlbum.Albums = []string{"harbour"}
	if got := validateContract(byAlbum); len(got) != 0 {
		t.Errorf("photo in an album reported %v", got)
	}
	byPlace := orphan
	byPlace.Locations = []string{"norway"}
	if got := validateContract(byPlace); len(got) != 0 {
		t.Errorf("photo with a place reported %v", got)
	}
}

func TestDedupeOriginalsCollapsesIdenticalCuration(t *testing.T) {
	taken := "2024-03-04T07:12:00"
	title := "Lamp"
	photo := func(source string) Photo {
		return Photo{
			Source: source, Published: true, Rating: 4, Title: &title, CapturedAt: &taken,
			File: FileInfo{SHA256: "abc123"},
		}
	}
	kept, collapsed := dedupeOriginals([]Photo{photo("inbox/copy.jpg"), photo("2023/11/lamp.jpg")})
	if collapsed != 1 {
		t.Fatalf("collapsed = %d, want 1", collapsed)
	}
	if len(kept) != 1 {
		t.Fatalf("kept %d photos, want 1", len(kept))
	}
	// The survivor is chosen by path so the index does not depend on walk order.
	if kept[0].Source != "2023/11/lamp.jpg" {
		t.Errorf("kept %q, want the first path in sort order", kept[0].Source)
	}
}

func TestDedupeOriginalsRefusesConflictingCuration(t *testing.T) {
	taken := "2024-03-04T07:12:00"
	first, second := "First", "Second"
	message := catchFail(t, func() {
		dedupeOriginals([]Photo{
			{Source: "a.jpg", Rating: 4, Title: &first, CapturedAt: &taken, File: FileInfo{SHA256: "dup"}},
			{Source: "b.jpg", Rating: 5, Title: &second, CapturedAt: &taken, File: FileInfo{SHA256: "dup"}},
		})
	})
	if !strings.Contains(message, "different metadata") {
		t.Errorf("fail() said %q, want it to mention the disagreement", message)
	}
}

func TestCompareSlicesOrdersHierarchies(t *testing.T) {
	// A parent sorts before its own child, and siblings alphabetically.
	cases := []struct {
		a, b []string
		want int
	}{
		{[]string{"Places"}, []string{"Places", "Harbour"}, -1},
		{[]string{"Places", "Apple"}, []string{"Places", "Harbour"}, -1},
		{[]string{"Places", "Harbour"}, []string{"Places", "Harbour"}, 0},
		{[]string{"Subject"}, []string{"Places"}, 1},
	}
	for _, c := range cases {
		if got := compareSlices(c.a, c.b); sign(got) != c.want {
			t.Errorf("compareSlices(%v, %v) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func TestParseDateTimeRefusesWhatIsNotADate(t *testing.T) {
	// R-8: a date the index accepts travels into Hugo's front matter, which
	// refuses to parse an impossible one -- four stages away from the
	// photograph that caused it.
	good := map[string]string{
		"2024:03:04 05:06:07":       "2024-03-04T05:06:07", // EXIF writes colons
		"2024-03-04T05:06:07":       "2024-03-04T05:06:07", // XMP writes dashes
		"2024:03:04 05:06:07+02:00": "2024-03-04T05:06:07", // an offset is dropped
		"2024:03:04 05:06:07.25":    "2024-03-04T05:06:07", // and so is sub-second
		"2024:02:29 00:00:00":       "2024-02-29T00:00:00", // a leap day is a day
	}
	for source, want := range good {
		got := parseDateTime(&source)
		if got == nil || *got != want {
			t.Errorf("parseDateTime(%q) = %v, want %q", source, deref(got), want)
		}
	}

	for _, source := range []string{
		"0000:00:00 00:00:00", // a camera that lost its clock
		"2024:13:04 05:06:07", // no thirteenth month
		"2024:03:32 05:06:07", // no thirty-second day
		"2023:02:29 00:00:00", // 2023 was not a leap year
		"2024:03:04 25:00:00", // no twenty-fifth hour
		"not a date at all",
		"",
	} {
		if got := parseDateTime(&source); got != nil {
			t.Errorf("parseDateTime(%q) = %q, want nothing", source, *got)
		}
	}

	if got := parseDateTime(nil); got != nil {
		t.Errorf("parseDateTime(nil) = %q, want nothing", *got)
	}
}

func TestLocationTitleIsItsFilesOrItsHierarchy(t *testing.T) {
	// R-10: a place's file may carry a title. Without one, a place is named by
	// its whole hierarchy (R-6), not by its last level, so that two places that
	// share a leaf read differently in a list.
	library := t.TempDir()
	if err := os.MkdirAll(filepath.Join(library, libraryData, "locations"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(library, libraryData, "locations", "norway-bergen.yaml"),
		"tag: \"Location|Norway|Bergen\"\ntitle: Bergen, on the west coast\n")
	paths := [][]string{{"Norway", "Bergen"}, {"Norway", "Oslo"}, {"Germany", "Oslo"}}
	photo := Photo{Published: true, locationPaths: paths}

	titles := map[string]string{}
	for _, loc := range loadLocations(library, []Photo{photo}) {
		titles[loc.Slug] = loc.Title
	}
	for slug, want := range map[string]string{
		"norway-bergen": "Bergen, on the west coast",
		"norway-oslo":   "Norway » Oslo",
		"germany-oslo":  "Germany » Oslo",
		"norway":        "Norway",
	} {
		if titles[slug] != want {
			t.Errorf("location %s is titled %q, want %q", slug, titles[slug], want)
		}
	}
}
