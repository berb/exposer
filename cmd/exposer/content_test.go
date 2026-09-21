package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFocalLengthReadsBothSpellings(t *testing.T) {
	cases := []struct {
		name string
		want float64
		ok   bool
	}{
		{"TEST 12-40mm F2.8", 12, true},      // a zoom sits at its wide end
		{"OLYMPUS M.75mm F1.8", 75, true},    // the "M." prefix is not a decimal
		{"7.5mm f3.5", 7.5, true},            // fractional focal length
		{"LUMIX G VARIO 7-14/F4.0", 7, true}, // no "mm" at all: Panasonic's form
		{"LUMIX G 20/F1.7", 20, true},        //
		{"500mm", 500, true},                 //
		{"M.300mm F4.0 + MC-20", 300, true},  // a teleconverter in the name
		{"45-150mm f4.0-5.6", 45, true},      // the aperture range is not a focal
		{"TESTCAM A1", 0, false},             // a body, not a lens
		{"Some Prime f/1.4", 0, false},       // an aperture alone never counts
	}
	for _, c := range cases {
		got, ok := focalLength(c.name)
		if ok != c.ok || got != c.want {
			t.Errorf("focalLength(%q) = %v, %v; want %v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestGearOrderPutsCamerasFirstThenFocalLength(t *testing.T) {
	titles := map[string]string{
		"c1": "TESTCAM B2", "c2": "TESTCAM A1",
		"l1": "TEST 12-40mm F2.8", "l2": "TEST 7-14/F4.0",
		"l3": "TEST 45mm F1.8", "l4": "Mystery Glass",
	}
	kinds := map[string]string{
		"c1": gearCamera, "c2": gearCamera,
		"l1": gearLens, "l2": gearLens, "l3": gearLens, "l4": gearLens,
	}
	gear := map[string]bool{"c1": true, "c2": true, "l1": true, "l2": true, "l3": true, "l4": true}

	got := gearOrder([]string{"l3", "c1", "l4", "l1", "c2", "l2"}, gear, kinds, titles)
	want := []string{
		"c2", "c1", // cameras, alphabetical by title
		"l2", "l1", "l3", // 7, 12, 45
		"l4", // no focal length in the name: last
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gearOrder = %v, want %v", got, want)
	}
}

func TestGearOrderDropsWhatIsNotGear(t *testing.T) {
	got := gearOrder([]string{"subject-street", "c1"},
		map[string]bool{"c1": true},
		map[string]string{"c1": gearCamera},
		map[string]string{"c1": "TESTCAM A1"})
	if !reflect.DeepEqual(got, []string{"c1"}) {
		t.Errorf("gearOrder = %v, want only the gear tag", got)
	}
}

func TestListingKindAnswersHalfTheReachabilityQuestion(t *testing.T) {
	cases := []struct{ path, want string }{
		{"photos/timeline", "timeline"},
		{"photos/2024", "timeline"},
		{"photos/2024/03", "timeline"},
		{"photos/albums/harbour", "other"},
		{"photos/locations/norway", "other"},
		{"photos/tags/subject-street", "other"},
		// A scoped page links to its neighbours; counting it would let one
		// photograph vouch for the next.
		{"photos/albums/harbour/abc123", ""},
		{"photos/2024/03/abc123", ""},
		// These hold every photograph, so letting them answer is vacuous.
		{"photos/all", ""},
		{"photos/tags/gear-camera-testcam-a1", ""},
		{"photos/gear", ""},
		{"photos/p/abc123", ""},
		{"photos", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := listingKind(c.path); got != c.want {
			t.Errorf("listingKind(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestAlbumYearsCollapsesASingleYear(t *testing.T) {
	year := func(s string) *string { return &s }
	if got := albumYears(Album{FirstYear: year("2024"), LastYear: year("2024")}); got != "2024" {
		t.Errorf("albumYears = %q, want %q", got, "2024")
	}
	if got := albumYears(Album{FirstYear: year("2023"), LastYear: year("2025")}); got != "2023–2025" {
		t.Errorf("albumYears = %q, want a range", got)
	}
	if got := albumYears(Album{}); got != "" {
		t.Errorf("albumYears = %q, want empty for an album with no dated photographs", got)
	}
}

func TestReversedAndWithout(t *testing.T) {
	in := []string{"a", "b", "c"}
	if got := reversed(in); !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Errorf("reversed = %v", got)
	}
	if !reflect.DeepEqual(in, []string{"a", "b", "c"}) {
		t.Errorf("reversed modified its input: %v", in)
	}
	got := without([]string{"a", "gear-camera-x", "b"}, map[string]bool{"gear-camera-x": true})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("without = %v", got)
	}
}

func TestSplitFrontMatter(t *testing.T) {
	meta, body := splitFrontMatter("---\ntitle: About\nmenu: true\n---\n\nSome prose.\n")
	if meta.Title != "About" || !meta.Menu {
		t.Errorf("meta = %+v", meta)
	}
	if strings.TrimSpace(body) != "Some prose." {
		t.Errorf("body = %q", body)
	}

	// A file with no front matter is all body, not an error.
	meta, body = splitFrontMatter("Just prose.\n")
	if meta.Title != "" || strings.TrimSpace(body) != "Just prose." {
		t.Errorf("meta = %+v, body = %q", meta, body)
	}
}

func TestGearDescriptionIsOptional(t *testing.T) {
	dir := t.TempDir()
	if got := gearDescription(dir, "testcam-a1"); got != "" {
		t.Errorf("a library with no gear directory returned %q", got)
	}

	gear := filepath.Join(dir, libraryData, "gear")
	if err := os.MkdirAll(gear, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(gear, "testcam-a1.md"), "---\ntitle: ignored\n---\n\nThe body.\n")
	if got := gearDescription(dir, "testcam-a1"); got != "The body." {
		t.Errorf("gearDescription = %q, want the body with its front matter stripped", got)
	}
}

func TestCheckGearDescriptionsRefusesAFileNamingNothing(t *testing.T) {
	dir := t.TempDir()
	gear := filepath.Join(dir, libraryData, "gear")
	if err := os.MkdirAll(gear, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(gear, "testcam-a1.md"), "Fine.\n")
	write(t, filepath.Join(gear, "nonexistent.md"), "Names no camera and no lens.\n")

	known := map[string]string{"testcam-a1": "gear-camera-testcam-a1"}
	message := catchFail(t, func() { checkGearDescriptions(dir, known) })
	if !strings.Contains(message, "nonexistent") || !strings.Contains(message, "R-17") {
		t.Errorf("fail() said %q, want it to name the file and the requirement", message)
	}

	// With every file matched, it says nothing.
	known["nonexistent"] = "gear-lens-nonexistent"
	if message := catchFail(t, func() { checkGearDescriptions(dir, known) }); message != "" {
		t.Errorf("fail() said %q for a library whose gear files all match", message)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTagIndexSamplesTheNewestAndSkipsGear(t *testing.T) {
	// F-26: eleven at most, newest first, and never a gear tag -- those have
	// their own page (F-24) and are derived rather than curated (R-15).
	many := make([]string, 0, 13)
	for i := 0; i < 13; i++ {
		many = append(many, fmt.Sprintf("id%02d", i))
	}
	byTag := map[string][]string{
		"subject-street":      many,
		"places-harbour":      {"a", "b"},
		"gear-camera-testcam": many,
	}
	gear := map[string]bool{"gear-camera-testcam": true}
	titles := map[string]string{
		"subject-street": "Subject » Street", "places-harbour": "Places » Harbour",
	}

	got := tagIndex(byTag, gear, titles, t.TempDir())
	if len(got) != 2 {
		t.Fatalf("tagIndex returned %d sections, want the 2 curated tags", len(got))
	}
	if got[0]["slug"] != "places-harbour" || got[1]["slug"] != "subject-street" {
		t.Errorf("sections are out of slug order: %v, %v", got[0]["slug"], got[1]["slug"])
	}
	if got[0]["title"] != "Places » Harbour" {
		t.Errorf("title = %v, want the full path", got[0]["title"])
	}

	// The small tag is whole, so the template offers no way past it.
	if ids := got[0]["ids"].([]string); len(ids) != 2 || got[0]["total"].(int) != 2 {
		t.Errorf("the two-photograph tag came back as %v of %v", ids, got[0]["total"])
	}
	// The large one is cut to eleven and still reports all thirteen.
	ids := got[1]["ids"].([]string)
	if len(ids) != tagSectionSample {
		t.Fatalf("sampled %d photographs, want %d", len(ids), tagSectionSample)
	}
	if got[1]["total"].(int) != 13 {
		t.Errorf("total = %v, want 13 -- the count is of the tag, not the sample", got[1]["total"])
	}
	if ids[0] != "id12" {
		t.Errorf("the sample starts at %q, want the newest photograph", ids[0])
	}
}
