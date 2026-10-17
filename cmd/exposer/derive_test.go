package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLadderNeverUpscales(t *testing.T) {
	widths := []int{200, 400, 800, 1600, 2400}
	cases := []struct {
		longEdge int
		want     []int
	}{
		{2400, []int{200, 400, 800, 1600, 2400}},
		{900, []int{200, 400, 800}},
		{400, []int{200, 400}},
		// Smaller than every configured width: the photograph is still
		// published, at its own size, rather than blown up or skipped.
		{150, []int{150}},
		{0, nil},
	}
	for _, c := range cases {
		if got := ladder(widths, c.longEdge); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ladder(long edge %d) = %v, want %v", c.longEdge, got, c.want)
		}
	}
}

func TestParamHashChangesWithEverythingItCovers(t *testing.T) {
	base := config{
		Derivatives: derivativeConfig{
			Widths:       []int{200, 400},
			SquareWidths: []int{200},
			Formats:      []string{"jpeg", "avif"},
			JPEGQuality:  82,
			AVIFQuality:  50,
		},
	}
	tools := map[string]string{"magick": "7.1", "exiftool": "13.50"}
	original := paramHash(base, tools)

	if paramHash(base, tools) != original {
		t.Fatal("paramHash is not stable for identical inputs (F-2, F-3)")
	}

	// Every one of these changes the bytes a derivative would have, so every
	// one has to invalidate the cache.
	changed := map[string]config{}
	c := base
	c.Derivatives.Widths = []int{200, 400, 800}
	changed["widths"] = c
	c = base
	c.Derivatives.SquareWidths = []int{200, 400}
	changed["square widths"] = c
	c = base
	c.Derivatives.Formats = []string{"jpeg"}
	changed["formats"] = c
	c = base
	c.Derivatives.JPEGQuality = 81
	changed["jpeg quality"] = c
	c = base
	c.Derivatives.AVIFQuality = 51
	changed["avif quality"] = c

	for what, cfg := range changed {
		if paramHash(cfg, tools) == original {
			t.Errorf("changing the %s did not change the parameter hash", what)
		}
	}

	// The tools render the pixels, so their versions count too.
	for _, tool := range []string{"magick", "exiftool"} {
		newer := map[string]string{"magick": "7.1", "exiftool": "13.50"}
		newer[tool] += "-next"
		if paramHash(base, newer) == original {
			t.Errorf("a new %s did not change the parameter hash", tool)
		}
	}
}

func TestPublicNameCarriesShardIdAndHash(t *testing.T) {
	// B-10: the name says which photograph, which size and which bytes.
	derivs := []derivative{
		{Cache: "e7/e7a82ee2493d05a7/1a2b3c4d5e6f/800.avif"},
		{Cache: "e7/e7a82ee2493d05a7/1a2b3c4d5e6f/400sq.jpg"},
	}
	hashes := map[string]string{"800.avif": "244ad8cf", "400sq.jpg": "ac6fc20a"}
	namePublic("e7a82ee2493d05a7", derivs, hashes)

	want := []string{
		"photos/img/e7/e7a82ee2493d05a7/e7a82ee2493d05a7-800.244ad8cf.avif",
		"photos/img/e7/e7a82ee2493d05a7/e7a82ee2493d05a7-400sq.ac6fc20a.jpg",
	}
	for i, d := range derivs {
		if d.Public != want[i] {
			t.Errorf("public name %q, want %q", d.Public, want[i])
		}
		if !derivativeName.MatchString(d.Public) {
			t.Errorf("%q is a name assembly would refuse", d.Public)
		}
	}
}

func rightsPhoto(creator, rights string) Photo {
	photo := Photo{ID: "ab12cd34ef56ab78", Source: "x.jpg"}
	w, h := 400, 300
	photo.File.Width, photo.File.Height = &w, &h
	if creator != "" {
		photo.Rights.Creator = &creator
	}
	if rights != "" {
		photo.Rights.Rights = &rights
	}
	return photo
}

func TestRenderKeyFollowsWhatIsStamped(t *testing.T) {
	// F-16 puts creator and rights into every derivative, so a JPEG whose rights
	// change must not be served from the cache keyed by the old ones.
	const hash = "0123456789ab"
	key := func(creator, rights string) string {
		t.Helper()
		got, err := renderKeyFor(rightsPhoto(creator, rights), "x.jpg", hash)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := key("", ""); got != hash {
		t.Errorf("a photograph that stamps nothing has key %q, want the settings hash %q", got, hash)
	}
	seen := map[string]string{}
	for _, c := range []struct{ name, creator, rights string }{
		{"creator", "A. Photographer", ""},
		{"rights", "", "(c) 2024"},
		{"both", "A. Photographer", "(c) 2024"},
		{"changed rights", "A. Photographer", "(c) 2025"},
		{"changed creator", "B. Photographer", "(c) 2024"},
	} {
		got := key(c.creator, c.rights)
		if got == hash {
			t.Errorf("%s: key is the settings hash, so the rights are not part of it", c.name)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s and %s share cache key %s", c.name, other, got)
		}
		seen[got] = c.name
	}

	// The two fields are separate: "ab" + "c" is not "a" + "bc".
	if key("ab", "c") == key("a", "bc") {
		t.Error("creator and rights run together in the key")
	}
}

func TestRenderKeyOfARawStillFollowsItsSidecar(t *testing.T) {
	library := t.TempDir()
	raw := filepath.Join(library, "x.orf")
	write(t, raw, "raw")
	write(t, raw+".xmp", "one")
	photo := rightsPhoto("", "")
	photo.Source = "x.orf"

	first, err := renderKeyFor(photo, raw, "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	write(t, raw+".xmp", "two")
	second, err := renderKeyFor(photo, raw, "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("editing a RAW's sidecar left its cache key as it was")
	}
	if err := os.Remove(raw + ".xmp"); err != nil {
		t.Fatal(err)
	}
	if _, err := renderKeyFor(photo, raw, "0123456789ab"); err == nil {
		t.Error("a RAW without a sidecar has no development to key on, and must say so")
	}
}

func TestDeriveRendersAgainWhenAJPEGsRightsChange(t *testing.T) {
	// A complete cache entry is served without rendering. The entry's key must
	// therefore change when the rights do, or the old copyright line outlives
	// its correction under a name a host may cache forever.
	cfg := defaultConfig()
	cfg.Derivatives.Widths = []int{200, 400}
	cfg.Derivatives.SquareWidths = []int{200}
	cfg.Derivatives.Formats = []string{"jpeg"}

	library, cache := t.TempDir(), t.TempDir()
	const hash = "0123456789ab"
	before := rightsPhoto("A. Photographer", "(c) 2024")

	key, err := renderKeyFor(before, filepath.Join(library, before.Source), hash)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cache, before.ID[:2], before.ID, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"200.jpg", "400.jpg", "200sq.jpg"} {
		write(t, filepath.Join(dir, name), "rendered")
	}
	write(t, filepath.Join(dir, "meta.json"), `{
	  "dims": {"200.jpg": [200, 150], "400.jpg": [400, 300], "200sq.jpg": [200, 200]},
	  "tone": "#aabbcc",
	  "hashes": {"200.jpg": "00000001", "400.jpg": "00000002", "200sq.jpg": "00000003"}
	}`)

	_, _, fresh, err := derivePhoto(before, library, cache, hash, cfg, t.TempDir())
	if err != nil || fresh {
		t.Fatalf("an unchanged photograph should come from the cache: fresh=%v err=%v", fresh, err)
	}

	after := rightsPhoto("A. Photographer", "(c) 2025")
	_, _, fresh, err = derivePhoto(after, library, cache, hash, cfg, t.TempDir())
	// Without ImageMagick the render itself fails, which is as good a proof as
	// a render: what must not happen is a quiet cache hit.
	if err == nil && !fresh {
		t.Error("corrected rights were served from the cache entry stamped with the old ones")
	}
}
