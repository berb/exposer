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

	// With an imprint named, the credit comes first, then a middot, then the
	// imprint, last.
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
	if !regexp.MustCompile(`Hugo</a></span> · <span><a href="[^"]*">Imprint</a></span>\s*</nav>`).MatchString(both) {
		t.Errorf("want the credit, a middot, then the imprint link, last:\n%s", both)
	}
	if strings.Contains(plain, " · ") {
		t.Errorf("without an imprint the middot has nothing to separate:\n%s", plain)
	}

	// The line is inline Markdown, so it can carry a link -- but Hugo drops
	// raw HTML in it, since it lands on every page of the site.
	marked := strings.Replace(string(raw), `footer_line: "© 2026 The Test Library"`,
		`footer_line: "© 2026 [The Test Library](https://example.com/) <script>alert(1)</script>"`, 1)
	if marked == string(raw) {
		t.Fatal("the fixture configuration no longer has the footer line to change")
	}
	if err := os.WriteFile(config, []byte(marked), 0o644); err != nil {
		t.Fatal(err)
	}
	target = t.TempDir()
	runBuild([]string{goodLibrary, "--target", target, "--config", config, "--hugo", hugoBinary(t)})

	linked := footer(filepath.Join(target, "site"))
	if !strings.Contains(linked, `<a href="https://example.com/">The Test Library</a>`) {
		t.Errorf("the footer line's Markdown link was not rendered:\n%s", linked)
	}
	if strings.Contains(linked, "<script>") {
		t.Errorf("raw HTML in the footer line reached the page:\n%s", linked)
	}
	if strings.Contains(linked, "<p>") {
		t.Errorf("a one-line footer was wrapped in a paragraph:\n%s", linked)
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

func TestFontsAndScriptAreNamedForTheirContents(t *testing.T) {
	// B-10: a host may cache these forever, so each carries 8 hex digits of its
	// own SHA-256, every page asks for it by that name, and the unhashed name
	// is not published for a page to fall back on.
	site := buildSite(t, goodLibrary)
	hashed := regexp.MustCompile(`\.([0-9a-f]{8})\.(?:woff2|js)$`)

	var assets []string
	err := filepath.WalkDir(site, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".woff2" && ext != ".js" {
			return nil
		}
		rel, _ := filepath.Rel(site, path)
		m := hashed.FindStringSubmatch(rel)
		if m == nil {
			t.Errorf("%s is published without a hash in its name", rel)
			return nil
		}
		sum := sha256.Sum256(readBytes(t, path))
		if got := hex.EncodeToString(sum[:])[:8]; got != m[1] {
			t.Errorf("%s: contents hash to %s", rel, got)
		}
		assets = append(assets, "/"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 3 {
		t.Fatalf("published %v, want the two fonts and the script", assets)
	}

	page := string(readBytes(t, filepath.Join(site, "index.html")))
	photo, _ := filepath.Glob(filepath.Join(site, "photos", "p", "*", "index.html"))
	if len(photo) == 0 {
		t.Fatal("no photo page to read the script from")
	}
	page += string(readBytes(t, photo[0]))
	for _, asset := range assets {
		if !strings.Contains(page, asset) {
			t.Errorf("no page asks for %s", asset)
		}
	}
}

func TestFrontPageHeadingsAreNotLinks(t *testing.T) {
	// F-7: the album index and the whole timeline are one link beneath their
	// lists, so the headings above them are only headings.
	body := readFile(t, filepath.Join(buildSite(t, goodLibrary), "index.html"))
	for _, heading := range regexp.MustCompile(`<h2>.*?</h2>`).FindAllString(body, -1) {
		if strings.Contains(heading, "<a ") {
			t.Errorf("a front-page heading is a link: %s", heading)
		}
	}
	// F-7: the chips read as a photo page's do, without the top level.
	if !strings.Contains(body, `/photos/tags/places-harbour-dock/">Harbour » Dock</a>`) {
		t.Error("a front-page tag chip does not read as a photo page's chip does")
	}
	for _, link := range []string{
		`<a href="/photos/albums/">All albums →</a>`,
		`<a href="/photos/timeline/">Whole timeline →</a>`,
		`<a href="/photos/tags/">All tags →</a>`,
	} {
		if !strings.Contains(body, link) {
			t.Errorf("the front page lost %s", link)
		}
	}
}

func TestMenuListsTheSectionsInOrder(t *testing.T) {
	// F-21: the generated sections hold weights 1 to 5, in this order, so a
	// page of the photographer's own can be placed among them.
	body := readFile(t, filepath.Join(buildSite(t, goodLibrary), "index.html"))
	start := strings.Index(body, `<nav aria-label="Sections">`)
	end := strings.Index(body[max(start, 0):], "</nav>")
	if start < 0 || end < 0 {
		t.Fatal("no site menu on the front page")
	}
	var got []string
	for _, m := range regexp.MustCompile(`>([^<]+)</a>`).FindAllStringSubmatch(body[start:start+end], -1) {
		got = append(got, m[1])
	}
	// The fixture's about.md is in the menu too, with no weight, so after them.
	want := []string{"Albums", "Timeline", "All", "Tags", "Gear", "About"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the menu reads %v, want %v", got, want)
	}
}

func TestPhotoPageTagChipsDropTheTopLevel(t *testing.T) {
	// F-11: a chip carries the hierarchy without its top level, and a gear
	// chip only the equipment (R-15).
	site := buildSite(t, goodLibrary)
	body := readFile(t, filepath.Join(site, "photos", "p", "2e2d8e42959ed213", "index.html"))
	start := strings.Index(body, `<p class="meta tags">`)
	if start < 0 {
		t.Fatal("no tag chips on the photo page")
	}
	chips := body[start : start+strings.Index(body[start:], "</p>")]
	for _, want := range []string{
		`/photos/tags/places-harbour-dock/">Harbour » Dock</a>`, // Places|Harbour|Dock
		`/photos/tags/gear-camera-testcam-a1/">TESTCAM A1</a>`,  // Gear|Camera|TESTCAM A1
	} {
		if !strings.Contains(chips, want) {
			t.Errorf("the chips read %s, want one reading %s", chips, want)
		}
	}
	if strings.Contains(chips, "Places »") {
		t.Errorf("a chip still carries its top level: %s", chips)
	}
}
