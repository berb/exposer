package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The whole pipeline, from a library to the artifact that would be deployed,
// through the same runBuild a user's `exposer build` runs. It needs Hugo as
// well as exiftool and ImageMagick, and skips where Hugo cannot be had.

// hugoBinary is the pinned Hugo, fetched into the user cache if it is not
// there yet (B-8). Offline and uncached, the tests that render skip rather
// than fail: that is a machine without Hugo, not a broken pipeline.
func hugoBinary(t *testing.T) string {
	t.Helper()
	var path string
	if message := catchFail(t, func() { path = findHugo("") }); message != "" {
		missingTool(t, "no pinned Hugo available: %s", message)
	}
	return path
}

// buildSite runs every stage over a library and returns the artifact directory.
func buildSite(t *testing.T, library string) string {
	t.Helper()
	requireTools(t, "exiftool", "magick")
	hugo := hugoBinary(t)

	target := t.TempDir()
	runBuild([]string{library, "--target", target, "--hugo", hugo})
	return filepath.Join(target, "site")
}

func TestSiteHasThePagesTheLibraryEarns(t *testing.T) {
	site := buildSite(t, goodLibrary)

	for _, page := range []string{
		"index.html",                                // the index is the front page (§11 Q6)
		"photos/index.html",                         // and answers /photos/ as well
		"photos/albums/index.html",                  // F-7
		"photos/albums/harbour/index.html",          // the album itself
		"photos/albums/private-set/index.html",      // F-18: built, just not advertised
		"photos/timeline/index.html",                // F-10
		"photos/all/index.html",                     // F-23
		"photos/gear/index.html",                    // F-24
		"photos/tags/index.html",                    // F-26
		"photos/locations/norway/index.html",        // F-8a, the place without coordinates
		"photos/locations/norway-bergen/index.html", // and the one with them
		"photos/tags/subject-street/index.html",     // F-8
		"photos/2024/index.html",                    // the year
		"photos/2024/03/index.html",                 // and the month
		"photos/about/index.html",                   // R-13's standalone page
		"sitemap.xml", "index.xml",                  // F-19
	} {
		if _, err := os.Stat(filepath.Join(site, page)); err != nil {
			t.Errorf("missing %s", page)
		}
	}

	// R-7's filtered tags must not have become pages of their own.
	for _, absent := range []string{"photos/tags/todo", "photos/tags/darktable-exported"} {
		if _, err := os.Stat(filepath.Join(site, absent)); err == nil {
			t.Errorf("%s exists; R-7 filters that tag", absent)
		}
	}
}

func TestUnlistedAlbumIsReachableButNotAdvertised(t *testing.T) {
	site := buildSite(t, goodLibrary)

	// F-18: the page exists...
	if _, err := os.Stat(filepath.Join(site, "photos/albums/private-set/index.html")); err != nil {
		t.Fatalf("the unlisted album has no page: %v", err)
	}
	// ...and nothing links to it.
	for _, page := range []string{"index.html", "photos/albums/index.html", "sitemap.xml", "index.xml"} {
		body := readFile(t, filepath.Join(site, page))
		if strings.Contains(body, "albums/private-set/") {
			t.Errorf("%s links to the unlisted album", page)
		}
	}
}

func TestBuildIsByteIdentical(t *testing.T) {
	// F-3: the same library must produce the same bytes, or a deploy cannot
	// tell a real change from a rebuild.
	first := hashTree(t, buildSite(t, goodLibrary), ".html")
	second := hashTree(t, buildSite(t, goodLibrary), ".html")

	if len(first) != len(second) {
		t.Fatalf("%d pages, then %d", len(first), len(second))
	}
	for path, sum := range first {
		if other, ok := second[path]; !ok {
			t.Errorf("%s appeared only in the first build", path)
		} else if other != sum {
			t.Errorf("%s differs between builds", path)
		}
	}
}

func TestAssemblyRefusesALeakedOriginal(t *testing.T) {
	// F-15 is enforced by hashing the artifact, so the way to test it is to put
	// an original into one and watch the check find it.
	site := buildSite(t, goodLibrary)
	original := readBytes(t, filepath.Join(goodLibrary, "2024/03/harbour-dawn.jpg"))
	planted := filepath.Join(site, "photos", "leaked.jpg")
	if err := os.WriteFile(planted, original, 0o644); err != nil {
		t.Fatal(err)
	}

	index := filepath.Join(filepath.Dir(site), "index.json")
	leaked := checkNoOriginals(site, index)
	if len(leaked) != 1 || !strings.Contains(leaked[0], "leaked.jpg") {
		t.Errorf("checkNoOriginals found %v, want the planted original", leaked)
	}
}

func TestAssemblyRefusesAnUnreachablePhotograph(t *testing.T) {
	// §12 criterion 4. Emptying the album listing strands the photographs whose
	// only curated path it was.
	site := buildSite(t, goodLibrary)
	index := filepath.Join(filepath.Dir(site), "index.json")

	if unreachable := checkReachable(site, index); len(unreachable) != 0 {
		t.Fatalf("the untouched artifact reported %v", unreachable)
	}

	// One photograph in the library has its album as its only curated path, so
	// emptying that listing must strand exactly it. Anything else in the album
	// is also on a tag page or a place, and stays reachable.
	listing := filepath.Join(site, "photos", "albums", "harbour", "index.html")
	write(t, listing, "<html><body>nothing here</body></html>")

	unreachable := checkReachable(site, index)
	if len(unreachable) != 1 {
		t.Fatalf("emptying the album listing stranded %v, want exactly the photograph whose only path it was", unreachable)
	}
	if !strings.Contains(unreachable[0], "scanned-print.jpg") ||
		!strings.Contains(unreachable[0], "only on the timeline") {
		t.Errorf("stranded %q, want scanned-print.jpg with its reason", unreachable[0])
	}
}

func TestGearDescriptionsAreCheckedAgainstTheLibrary(t *testing.T) {
	// R-17's failure lands in the content stage, which is the first that knows
	// what gear the library actually holds.
	requireTools(t, "exiftool", "magick")

	dir, _ := buildDerivatives(t, filepath.Join(brokenRoot, "unknown-gear"))
	libraryAbs, err := filepath.Abs(filepath.Join(brokenRoot, "unknown-gear"))
	if err != nil {
		t.Fatal(err)
	}

	message := catchFail(t, func() {
		runContent([]string{
			"--index", filepath.Join(dir, "index.json"),
			"--manifest", filepath.Join(dir, "derivatives.json"),
			"--library", libraryAbs,
			"--out", filepath.Join(dir, "hugo"),
		})
	})
	if !strings.Contains(message, "nonexistent") || !strings.Contains(message, "camera or lens") {
		t.Errorf("content said %q, want it to name the file and the requirement", message)
	}
}

func TestTagDescriptionsAreCheckedAgainstTheLibrary(t *testing.T) {
	// R-19, the same bargain R-17 strikes for gear: a description nobody can
	// ever see is worse than no description, so the build stops.
	requireTools(t, "exiftool", "magick")

	dir, _ := buildDerivatives(t, filepath.Join(brokenRoot, "unknown-tag"))
	libraryAbs, err := filepath.Abs(filepath.Join(brokenRoot, "unknown-tag"))
	if err != nil {
		t.Fatal(err)
	}

	message := catchFail(t, func() {
		runContent([]string{
			"--index", filepath.Join(dir, "index.json"),
			"--manifest", filepath.Join(dir, "derivatives.json"),
			"--library", libraryAbs,
			"--out", filepath.Join(dir, "hugo"),
		})
	})
	if !strings.Contains(message, "nonexistent") || !strings.Contains(message, "no tag") {
		t.Errorf("content said %q, want it to name the file and the requirement", message)
	}
}

func TestTagDescriptionReachesBothPagesThatShowIt(t *testing.T) {
	// R-19 is written once and read in two places: the tag index (F-26) and the
	// tag's own listing (F-8). A change that feeds only one of them is the
	// regression this catches.
	site := buildSite(t, goodLibrary)

	for _, page := range []string{
		"photos/tags/index.html",
		"photos/tags/subject-street/index.html",
	} {
		body := readFile(t, filepath.Join(site, page))
		// Markdown, not the source text: the em is what proves it was rendered.
		if !strings.Contains(body, "<em>in public</em>") {
			t.Errorf("%s does not render the tag description", page)
		}
	}

	// A tag the library says nothing about carries no empty note.
	body := readFile(t, filepath.Join(site, "photos/tags/shapes-square/index.html"))
	if strings.Contains(body, `class="note"`) {
		t.Errorf("an undescribed tag got a note wrapper anyway")
	}
}

func hashTree(t *testing.T, root, suffix string) map[string]string {
	t.Helper()
	sums := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, suffix) {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		h := sha256.New()
		if _, err := io.Copy(h, file); err != nil {
			return err
		}
		sums[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) == 0 {
		t.Fatalf("no %s files under %s", suffix, root)
	}
	return sums
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	return string(readBytes(t, path))
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var _ = sort.Strings

func TestFooterCarriesTheLineTheImprintAndTheCredit(t *testing.T) {
	// D-10. The fixture's own configuration sets a footer line and no imprint.
	footer := func(site string) string {
		t.Helper()
		body := readFile(t, filepath.Join(site, "index.html"))
		start, end := strings.Index(body, "<footer"), strings.Index(body, "</footer>")
		if start < 0 || end < start {
			t.Fatalf("no footer on the index")
		}
		return body[start:end]
	}
	credit := `Generated by <a href="https://github.com/berb/exposer">exposer</a> and <a href="https://gohugo.io/">Hugo</a>`

	plain := footer(buildSite(t, goodLibrary))
	if !strings.Contains(plain, "© 2026 The Test Library") {
		t.Errorf("the footer line is missing:\n%s", plain)
	}
	if !strings.Contains(plain, credit) {
		t.Errorf("the credit is missing:\n%s", plain)
	}

	// With an imprint named, it comes first, then the credit.
	requireTools(t, "exiftool", "magick")
	config := filepath.Join(t.TempDir(), configFileName)
	raw, err := os.ReadFile(filepath.Join(goodLibrary, libraryData, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	withImprint := strings.Replace(string(raw), `imprint_page: ""`, `imprint_page: "about"`, 1)
	if withImprint == string(raw) {
		t.Fatal("the fixture configuration no longer has imprint_page to set")
	}
	if err := os.WriteFile(config, []byte(withImprint), 0o644); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	runBuild([]string{goodLibrary, "--target", target, "--config", config, "--hugo", hugoBinary(t)})

	both := footer(filepath.Join(target, "site"))
	imprint, generated := strings.Index(both, ">Imprint</a>"), strings.Index(both, "Generated by")
	if imprint < 0 || generated < 0 || imprint > generated {
		t.Errorf("want the imprint link, then the credit:\n%s", both)
	}
}

func TestPhotoSizesFollowTheLayout(t *testing.T) {
	// A browser picks from srcset before layout, from `sizes` alone, so the
	// hint in viewersizes.html restates the photo page's CSS as numbers. If
	// the CSS changes and the hint does not, nothing looks wrong -- the
	// browser quietly fetches the wrong file again. So the rules the hint
	// depends on are pinned here: change one, and this names the partial to
	// update with it.
	css := readFile(t, filepath.Join("../../site-gen/layouts", "baseof.html"))
	for _, rule := range []string{
		"clamp(16px, 5vw, 72px)", // page margins: 5vw, 72px from 1440px on
		"--step: 8px",            // the gap below is 4 steps, 32px
		".detail { display: grid; grid-template-columns: 3fr 9fr; gap: calc(var(--step) * 4);",
		"max-width: min(100%, calc(78vh * var(--ar)))", // the height cap
		"@media (max-width: 800px) {\n  .detail { grid-template-columns: 1fr; }",
		"@media (max-width: 600px) {\n  body { padding: calc(var(--step) * 3) 0",
		".masthead, .colophon, main > :not(.grid) { padding-left: 16px; padding-right: 16px; }",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("baseof.html no longer has %q; update _partials/viewersizes.html to match the new layout, then this list", rule)
		}
	}

	// Each photo page states its own aspect ratio in the hint, the same one
	// the viewer's --ar uses for the height cap.
	site := buildSite(t, goodLibrary)
	pages, err := filepath.Glob(filepath.Join(site, "photos/p/*/index.html"))
	if err != nil || len(pages) == 0 {
		t.Fatalf("no photo pages: %v", err)
	}
	for _, page := range pages {
		body := readFile(t, page)
		ar := regexp.MustCompile(`--ar:([0-9.]+)`).FindStringSubmatch(body)
		if ar == nil {
			t.Fatalf("%s: no --ar on the viewer", page)
		}
		want, _ := strconv.ParseFloat(ar[1], 64)
		for _, m := range regexp.MustCompile(`calc\(78vh \* ([0-9.]+)\)`).FindAllStringSubmatch(body, -1) {
			got, _ := strconv.ParseFloat(m[1], 64)
			if math.Abs(got-want) > 0.0001 {
				t.Errorf("%s: sizes caps at 78vh * %s, but the viewer's ratio is %s", page, m[1], ar[1])
			}
		}
		if !strings.Contains(body, "calc(78vh * ") {
			t.Errorf("%s: the sizes hint does not know the height cap", page)
		}
	}
}
