package main

import (
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
