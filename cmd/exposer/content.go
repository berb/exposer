package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// renderPhoto is what a template needs to draw one photo. The index carries more
// than the site does; this is the subset, written once into data/photos.json and
// referenced by id from every listing (F-3: one copy, one ordering).
type renderPhoto struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Caption     string       `json:"caption"`
	Alt         string       `json:"alt"`
	CapturedAt  string       `json:"captured_at"`
	Year        string       `json:"year"`
	Month       string       `json:"month"`
	Tone        string       `json:"tone"`
	Width       int          `json:"width"`
	Height      int          `json:"height"`
	Tags        []tagRef     `json:"tags"`
	Albums      []albumRef   `json:"albums"`
	Locations   []placeRef   `json:"locations"`
	Camera      Camera       `json:"camera"`
	Exposure    Exposure     `json:"exposure"`
	Rights      Rights       `json:"rights"`
	Derivatives []derivative `json:"derivatives"`
}

// tagRef names a tag on a photo page. The top level is dropped -- a chip
// reading "Harbour » Dock" rather than "Places » Harbour » Dock" -- since the
// chips sit in a row of technical data where the whole path would crowd it,
// and what is left still tells two tags sharing a leaf apart. Gear keeps only
// the equipment, as its chips do everywhere (R-15).
type tagRef struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

// placeRef names a location on a photo the way a reader reads it — the whole
// hierarchy (R-6), not the slug the URL is built from.
type placeRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// albumRef names an album on a photo, so the album-independent page can list
// album titles rather than slugs.
type albumRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type page struct {
	path   string
	title  string
	date   string
	layout string
	params map[string]any
	body   string // R-13 pages carry Markdown; generated listings do not
	menu   bool   // F-21: listed in the site menu
	weight int
}

func runContent(args []string) {
	fs := flag.NewFlagSet("content", flag.ExitOnError)
	indexPath := fs.String("index", filepath.Join("target", "index.json"), "index to read")
	manifestPath := fs.String("manifest", filepath.Join("target", "derivatives.json"), "derivative manifest")
	library := fs.String("library", "source", "library root (read-only), for its standalone pages")
	theme := fs.String("theme", "", "theme directory to use instead of the built-in one")
	out := fs.String("out", filepath.Join("target", "hugo"), "generated hugo project")
	configPath := fs.String("config", "", "generator config (default: <library>/_data/exposer.yaml)")
	v, q := addOutputFlags(fs)
	fs.Parse(args)
	applyOutputFlags(v, q)

	cfg := configFor(*library, *configPath)
	doc := loadDocument(*indexPath)

	manifestData, err := os.ReadFile(*manifestPath)
	if err != nil {
		fail("cannot read %s: %v (run `exposer derive` first)", *manifestPath, err)
	}
	var manifest derivativeManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		fail("cannot parse %s: %v", *manifestPath, err)
	}

	albumTitles := map[string]string{}
	for _, album := range doc.Albums {
		albumTitles[album.Slug] = album.Title
	}
	placeTitles := map[string]string{}
	for _, loc := range doc.Locations {
		placeTitles[loc.Slug] = strings.Join(loc.Path, hierarchySeparator)
	}

	photos := map[string]renderPhoto{}
	var published []Photo
	for _, photo := range doc.Photos {
		if !photo.Published {
			continue
		}
		derivs := manifest.Photos[photo.ID]
		if len(derivs) == 0 {
			fail("no derivatives for %s — is the manifest stale?", photo.Source)
		}
		published = append(published, photo)
		photos[photo.ID] = toRenderPhoto(photo, derivs, manifest.Tones[photo.ID], albumTitles, placeTitles)
	}
	sort.Slice(published, func(i, j int) bool {
		if a, b := deref(published[i].CapturedAt), deref(published[j].CapturedAt); a != b {
			return a < b
		}
		return published[i].ID < published[j].ID
	})

	pages := buildPages(doc, published, photos, cfg, *library)
	standalone := loadLibraryPages(*library, pages)
	pages = append(pages, standalone...)

	if err := os.RemoveAll(*out); err != nil {
		fail("cannot clear %s: %v", *out, err)
	}
	writeJSON(filepath.Join(*out, "data", "photos.json"), photos)
	writeHugoConfig(filepath.Join(*out, "hugo.toml"), cfg)
	themeFiles := themeFS(*theme)
	copyTree(themeFiles, "layouts", filepath.Join(*out, "layouts"))
	// Fonts live here; without this they never reach the site.
	copyTree(themeFiles, "static", filepath.Join(*out, "static"))
	// fullscreen.js lives here, so Hugo can minify it on its way out.
	copyTree(themeFiles, "assets", filepath.Join(*out, "assets"))
	for _, p := range pages {
		writePage(filepath.Join(*out, "content", p.path), p)
	}

	report("%d photos, %d pages (%d standalone) -> %s",
		len(photos), len(pages), len(standalone), shown(*out))
}

func toRenderPhoto(photo Photo, derivs []derivative, tone string, albumTitles, placeTitles map[string]string) renderPhoto {
	sort.Slice(derivs, func(i, j int) bool {
		if derivs[i].Format != derivs[j].Format {
			return derivs[i].Format < derivs[j].Format
		}
		return derivs[i].Width < derivs[j].Width
	})
	captured := deref(photo.CapturedAt)
	width, height := 0, 0
	for _, d := range derivs {
		if d.Format == "jpeg" && d.Width > width {
			width, height = d.Width, d.Height
		}
	}
	return renderPhoto{
		ID: photo.ID, Title: deref(photo.Title), Caption: deref(photo.Caption),
		Alt: photo.Alt, CapturedAt: captured, Tone: tone,
		Year:  safeSlice(captured, 0, 4),
		Month: safeSlice(captured, 5, 7),
		Width: width, Height: height,
		Tags: tagRefs(photo.Tags), Albums: albumRefs(photo.Albums, albumTitles), Locations: placeRefs(photo.Locations, placeTitles),
		Camera: photo.Camera, Exposure: photo.Exposure, Rights: photo.Rights,
		Derivatives: derivs,
	}
}

func tagRefs(tags []Tag) []tagRef {
	refs := make([]tagRef, 0, len(tags))
	for _, tag := range tags {
		refs = append(refs, tagRef{Slug: tag.Slug, Label: tagChipLabel(tag)})
	}
	return refs
}

// tagChipLabel is what a chip says wherever one is written (F-11): the tag
// without its top level, and gear as the equipment alone (R-15).
func tagChipLabel(tag Tag) string {
	if len(tag.Path) > 1 && tag.Path[0] != gearNamespace {
		return strings.Join(tag.Path[1:], hierarchySeparator)
	}
	return tag.Leaf
}

func albumRefs(slugs []string, titles map[string]string) []albumRef {
	refs := make([]albumRef, 0, len(slugs))
	for _, slug := range slugs {
		title := titles[slug]
		if title == "" {
			title = slug
		}
		refs = append(refs, albumRef{Slug: slug, Title: title})
	}
	return refs
}

func placeRefs(slugs []string, titles map[string]string) []placeRef {
	refs := make([]placeRef, 0, len(slugs))
	for _, slug := range slugs {
		title := titles[slug]
		if title == "" {
			title = slug
		}
		refs = append(refs, placeRef{Slug: slug, Title: title})
	}
	return refs
}

func safeSlice(s string, from, to int) string {
	if len(s) < to {
		return ""
	}
	return s[from:to]
}

// buildPages maps the index onto the fixed URL layout (§11 Q6). Every page holds
// an ordered list of ids; the details live in data/photos.json.
func buildPages(doc Document, published []Photo, photos map[string]renderPhoto, cfg config, library string) []page {
	byAlbum := map[string][]string{}
	byTag := map[string][]string{}
	byLocation := map[string][]string{}
	byYear := map[string][]string{}
	byMonth := map[string][]string{}
	tagTitles := map[string]string{}
	// F-11's chip label: the hierarchy without its top level. A heading names
	// the tag in full; a chip is read in a row of other chips, where the top
	// level is the part that repeats.
	tagLabels := map[string]string{}
	placeTitles := map[string]string{}
	for _, loc := range doc.Locations {
		placeTitles[loc.Slug] = strings.Join(loc.Path, hierarchySeparator)
	}
	gearTags := map[string]bool{}
	gearTitles := map[string]string{}
	gearKinds := map[string]string{}

	for _, photo := range published {
		render := photos[photo.ID]
		for _, ref := range render.Albums {
			byAlbum[ref.Slug] = append(byAlbum[ref.Slug], photo.ID)
		}
		for _, tag := range photo.Tags {
			byTag[tag.Slug] = append(byTag[tag.Slug], photo.ID)
			tagTitles[tag.Slug] = strings.Join(tag.Path, hierarchySeparator)
			tagLabels[tag.Slug] = tagChipLabel(tag)
			if len(tag.Path) > 0 && tag.Path[0] == gearNamespace {
				gearTags[tag.Slug] = true
				// Under a heading that already says "Gear", repeating the
				// namespace on every chip says nothing: the leaf is the camera
				// or the lens, which is the whole of what the tag names. The
				// kind between them orders the list rather than being shown.
				gearTitles[tag.Slug] = tag.Leaf
				if len(tag.Path) > 1 {
					gearKinds[tag.Slug] = tag.Path[1]
				}
			}
		}
		for _, slug := range photo.Locations {
			byLocation[slug] = append(byLocation[slug], photo.ID)
		}
		if render.Year != "" {
			byYear[render.Year] = append(byYear[render.Year], photo.ID)
			if render.Month != "" {
				byMonth[render.Year+"/"+render.Month] = append(
					byMonth[render.Year+"/"+render.Month], photo.ID)
			}
		}
	}

	// F-8a: a photo at Germany|Ulm also belongs to Germany (R-6).
	locationsBySlug := map[string]Location{}
	for _, loc := range doc.Locations {
		locationsBySlug[loc.Slug] = loc
	}
	rolled := map[string][]string{}
	for _, loc := range doc.Locations {
		seen := map[string]bool{}
		for _, other := range doc.Locations {
			if len(other.Path) < len(loc.Path) ||
				compareSlices(other.Path[:len(loc.Path)], loc.Path) != 0 {
				continue
			}
			for _, id := range byLocation[other.Slug] {
				if !seen[id] {
					seen[id] = true
					rolled[loc.Slug] = append(rolled[loc.Slug], id)
				}
			}
		}
	}

	var pages []page
	add := func(p page) { pages = append(pages, p) }

	// Every listing behaves the same way: a section page, plus one scoped page
	// per photograph carrying that listing's context and neighbours.
	scoped := func(base []string, contextTitle string, ids []string) {
		prefix := strings.Join(base, "/")
		for i, id := range ids {
			add(page{
				path:  filepath.Join(append(append([]string{}, base...), id+".md")...),
				title: photos[id].Title, date: photos[id].CapturedAt, layout: "scoped",
				params: map[string]any{
					"id": id, "contextTitle": contextTitle, "contextPath": prefix + "/",
					"position": i + 1, "total": len(ids),
					// The neighbours travel as ids as well as paths: F-25 shows
					// each one as a thumbnail, which needs the photograph, not
					// just the way to it.
					"prev":      prefix + "/" + ids[(i-1+len(ids))%len(ids)] + "/",
					"next":      prefix + "/" + ids[(i+1)%len(ids)] + "/",
					"prevId":    ids[(i-1+len(ids))%len(ids)],
					"nextId":    ids[(i+1)%len(ids)],
					"canonical": fmt.Sprintf("photos/p/%s/", id),
					"sitemap":   false,
				},
			})
		}
	}

	albumIndex := make([]map[string]any, 0, len(doc.Albums))
	for _, album := range doc.Albums {
		ids := byAlbum[album.Slug]
		if len(ids) == 0 {
			continue
		}
		base := []string{"photos", "albums", album.Slug}
		add(page{
			path:  filepath.Join(append(append([]string{}, base...), "_index.md")...),
			title: album.Title, date: deref(album.Date), layout: "album",
			params: map[string]any{
				"ids": ids, "intro": deref(album.Intro), "slug": album.Slug,
				"unlisted": album.Unlisted, "cover": cover(album),
				"years":     albumYears(album),
				"scopeBase": strings.Join(base, "/"),
			},
		})
		scoped(base, album.Title, ids)

		// F-18: still built and still reachable by URL, just not advertised.
		if album.Unlisted {
			continue
		}
		// F-7: the album index shows a sample rather than one cover. These are
		// the album's first photographs in its own order (F-6), so the tile row
		// opens the way the album page does rather than jumping to its end.
		sample := ids
		if len(sample) > albumIndexSample {
			sample = sample[:albumIndexSample]
		}
		albumIndex = append(albumIndex, map[string]any{
			"slug": album.Slug, "title": album.Title, "date": deref(album.Date),
			"cover": cover(album), "count": len(ids), "years": albumYears(album),
			"intro": deref(album.Intro), "sample": sample,
		})
	}

	for _, slug := range sortedMapKeys(byTag) {
		base := []string{"photos", "tags", slug}
		params := map[string]any{"ids": byTag[slug], "kind": "tag", "slug": slug}
		// R-19: optional prose about the tag, rendered as Markdown. Gear keeps
		// its own files (R-17), so a gear tag is not described here.
		if !gearTags[slug] {
			if body := tagDescription(library, slug); body != "" {
				params["note"] = body
			}
		}
		// R-15's gear tags are derived, not curated. They list photographs, but
		// they are not a sequence anyone composed, so they get no scoped pages
		// and their grid links go to the album-independent page.
		if !gearTags[slug] {
			params["scopeBase"] = strings.Join(base, "/")
			scoped(base, tagTitles[slug], byTag[slug])
		}
		add(page{
			path:  filepath.Join(append(append([]string{}, base...), "_index.md")...),
			title: tagTitles[slug], layout: "listing", params: params,
		})
	}

	curatedTags := map[string]bool{}
	for _, slug := range without(sortedMapKeys(byTag), gearTags) {
		curatedTags[slug] = true
	}
	checkTagDescriptions(library, curatedTags)

	for _, slug := range sortedMapKeys(rolled) {
		loc := locationsBySlug[slug]
		base := []string{"photos", "locations", slug}
		title := placeTitles[slug]
		params := map[string]any{
			"ids": rolled[slug], "kind": "location", "slug": slug,
			"scopeBase": strings.Join(base, "/"),
		}
		if loc.Description != nil {
			params["description"] = *loc.Description
		}
		if loc.Lat != nil && loc.Lon != nil {
			params["lat"], params["lon"] = float64(*loc.Lat), float64(*loc.Lon)
		}
		add(page{
			path:  filepath.Join(append(append([]string{}, base...), "_index.md")...),
			title: title, layout: "listing", params: params,
		})
		scoped(base, title, rolled[slug])
	}

	// F-10's timeline as one page: every published photograph, newest first,
	// under a heading per year. The groups link into the year listings that
	// already exist, so nothing here adds a second set of scoped pages.
	timeline := make([]map[string]any, 0, len(byYear))
	years := sortedMapKeys(byYear)
	for i := len(years) - 1; i >= 0; i-- {
		year := years[i]
		ids := byYear[year]
		newest := make([]string, len(ids))
		for j, id := range ids {
			newest[len(ids)-1-j] = id
		}
		timeline = append(timeline, map[string]any{
			"year": year, "ids": newest, "base": "photos/" + year,
		})
	}
	add(page{
		path: filepath.Join("photos", "timeline", "_index.md"), title: "Timeline",
		layout: "timeline", menu: true, weight: 2,
		params: map[string]any{"groups": timeline, "total": len(published)},
	})

	// F-23: the whole library on one page, newest first, as square tiles. The
	// timeline says when; this one says how much, and it is the only page that
	// never groups. Tiles link to the album-independent page (F-11), so it adds
	// no scoped pages of its own.
	all := make([]string, 0, len(published))
	for _, photo := range published {
		all = append(all, photo.ID)
	}
	add(page{
		path: filepath.Join("photos", "all", "_index.md"), title: "All",
		layout: "all", menu: true, weight: 3,
		params: map[string]any{"ids": reversed(all), "total": len(published)},
	})

	// F-24: one section per camera and per lens, cameras first (R-15's kind is
	// what orders them). Each shows that gear's most recent photographs beside
	// whatever the library says about it, and hands off to the tag page, which
	// is the complete listing.
	gearGroups := make([]map[string]any, 0, 2)
	gearFiles := map[string]string{}
	lastKind := ""
	for _, slug := range gearOrder(sortedMapKeys(byTag), gearTags, gearKinds, gearTitles) {
		ids := reversed(byTag[slug])
		if len(ids) > gearSectionSample {
			ids = ids[:gearSectionSample]
		}
		name := slugify(gearTitles[slug])
		gearFiles[name] = slug
		section := map[string]any{
			"slug": slug, "title": gearTitles[slug], "kind": gearKinds[slug],
			"ids": ids, "total": len(byTag[slug]),
		}
		if body := gearDescription(library, name); body != "" {
			section["description"] = body
		}
		// gearOrder already groups by kind, cameras before lenses, so a change
		// of kind is the start of a group rather than something to sort for.
		if kind := gearKinds[slug]; kind != lastKind || len(gearGroups) == 0 {
			lastKind = kind
			gearGroups = append(gearGroups, map[string]any{
				"kind": gearGroupTitle(kind), "sections": []map[string]any{},
			})
		}
		group := gearGroups[len(gearGroups)-1]
		group["sections"] = append(group["sections"].([]map[string]any), section)
	}
	if len(gearGroups) > 0 {
		checkGearDescriptions(library, gearFiles)
		add(page{
			path: filepath.Join("photos", "gear", "_index.md"), title: "Gear",
			layout: "gear", menu: true, weight: 5,
			params: map[string]any{"groups": gearGroups},
		})
	}

	// F-26: the tag index, laid out as F-24 lays out gear. Only the curated
	// tags: R-15's gear tags have their own page, and locations and albums are
	// not tags by the time the index is written -- they became their own
	// namespaces in stage 1.
	tagSections := tagIndex(byTag, gearTags, tagLabels, library)
	if len(tagSections) > 0 {
		add(page{
			path: filepath.Join("photos", "tags", "_index.md"), title: "Tags",
			layout: "tags", menu: true, weight: 4,
			params: map[string]any{"tags": tagSections},
		})
	}

	for _, year := range sortedMapKeys(byYear) {
		base := []string{"photos", year}
		add(page{
			path:  filepath.Join(append(append([]string{}, base...), "_index.md")...),
			title: year, layout: "listing",
			params: map[string]any{
				"ids": byYear[year], "kind": "year", "slug": year,
				"scopeBase": strings.Join(base, "/"),
			},
		})
		scoped(base, year, byYear[year])
	}
	for _, key := range sortedMapKeys(byMonth) {
		year, month, _ := strings.Cut(key, "/")
		base := []string{"photos", year, month}
		title := year + "-" + month
		add(page{
			path:  filepath.Join(append(append([]string{}, base...), "_index.md")...),
			title: title, layout: "listing",
			params: map[string]any{
				"ids": byMonth[key], "kind": "month", "slug": key,
				"scopeBase": strings.Join(base, "/"),
			},
		})
		scoped(base, title, byMonth[key])
	}

	for _, photo := range published {
		// Prefer the title; alt falls back to the caption and then the id, which
		// makes a poor page title and a redundant og:title beside the description.
		heading := photos[photo.ID].Title
		if heading == "" {
			heading = photos[photo.ID].Alt
		}
		add(page{
			path:  filepath.Join("photos", "p", photo.ID+".md"),
			title: heading, date: deref(photo.CapturedAt), layout: "photo",
			params: map[string]any{"id": photo.ID},
		})
	}

	// F-7 plus the entry points, so nothing generated is unreachable. The index
	// is the site root: a front page whose only content is a link to the real
	// one wastes the first page a reader sees.
	// R-16: the photographer's own selection leads the index, in the order they
	// wrote it. With no list, fall back to a sample of the library — the ids are
	// content hashes, so ordering by id is unrelated to capture time, album or
	// name, and it reads as a random draw while staying the same every build (F-3).
	sample := append([]string{}, doc.Featured...)
	curated := len(sample) > 0
	if len(sample) == 0 {
		for _, photo := range published {
			sample = append(sample, photo.ID)
		}
		sort.Strings(sample)
	}
	if len(sample) > frontPageSample {
		sample = sample[:frontPageSample]
	}

	index := func() map[string]any {
		return map[string]any{
			"subtitle":  cfg.Subtitle,
			"albums":    albumIndex,
			"tags":      named(without(sortedMapKeys(byTag), gearTags), tagLabels),
			"gear":      named(gearOrder(sortedMapKeys(byTag), gearTags, gearKinds, gearTitles), gearTitles),
			"locations": named(sortedMapKeys(rolled), placeTitles),
			"years":     reversed(sortedMapKeys(byYear)),
			"sample":    sample,
			"curated":   curated,
		}
	}
	add(page{path: "_index.md", title: cfg.Title, layout: "home", params: index()})

	// F-7's album index also stands on its own, in the menu beside the R-13
	// pages (F-21). Hugo emits /photos/albums/ as a section page regardless,
	// so writing it is what keeps it from falling back to a bare list.
	add(page{
		path: filepath.Join("photos", "albums", "_index.md"), title: "Albums",
		layout: "albums", menu: true, weight: 1,
		params: map[string]any{"albums": albumIndex},
	})

	// §11 Q6 keeps every generated URL under /photos/, and Hugo emits the
	// section page whether or not we write one, so /photos/ answers with the
	// same index rather than a stub. It points at the root the way every other
	// duplicate does: rel=canonical, and out of the sitemap (F-19).
	photosIndex := index()
	photosIndex["canonical"] = ""
	photosIndex["sitemap"] = false
	add(page{
		path: filepath.Join("photos", "_index.md"), title: cfg.Title,
		layout: "overview", params: photosIndex,
	})
	return pages
}

// hierarchySeparator joins the levels of a hierarchical tag (R-6) wherever one
// is written out: "Gear » NIKON Z 6_2", "Germany » Ulm".
const hierarchySeparator = " \u00bb "

// frontPageSample is how many photographs the index shows beside its lists.
const frontPageSample = 24

// albumIndexSample is how many photographs each album shows on the album index.
// Eleven, so the twelfth cell of the grid can be the way into the album itself
// (F-7) -- always, here, because an album is a place to go rather than a sample
// to skim, however few photographs it holds.
const albumIndexSample = 11

// gearSectionSample is how many photographs each gear section shows. Eleven,
// not twelve: when there are more, the twelfth cell of the grid is the way to
// the rest (F-24), so the row stays whole either way.
const gearSectionSample = 11

// tagIndex builds F-26's sections: one per curated tag, in slug order, each
// carrying its most recent photographs and how many there are in total, so the
// template can offer the way to the rest without counting again.
// tagIndex builds F-26's sections. Each wears the chip F-11 describes, so a
// tag reads the same here, on the front page and on a photograph.
func tagIndex(byTag map[string][]string, gear map[string]bool, labels map[string]string, library string) []map[string]any {
	out := make([]map[string]any, 0, len(byTag))
	for _, slug := range without(sortedMapKeys(byTag), gear) {
		ids := reversed(byTag[slug])
		if len(ids) > tagSectionSample {
			ids = ids[:tagSectionSample]
		}
		section := map[string]any{
			"slug": slug, "title": labels[slug],
			"ids": ids, "total": len(byTag[slug]),
		}
		// R-19: the same prose the tag's own listing carries, so a reader meets
		// one description of a tag rather than two.
		if body := tagDescription(library, slug); body != "" {
			section["description"] = body
		}
		out = append(out, section)
	}
	return out
}

// tagSectionSample is how many photographs each tag shows on the tag index
// (F-26). The same eleven the gear page shows, and for the same reason: the
// twelfth cell is the way to the full listing when there is more to see.
const tagSectionSample = 11

// gearGroupTitle names the heading a kind of gear sits under. R-15 records the
// kind in the singular, because it qualifies one camera or one lens; the
// heading stands over all of them.
func gearGroupTitle(kind string) string {
	switch kind {
	case gearCamera:
		return "Cameras"
	case gearLens:
		return "Lenses"
	default:
		return "Other"
	}
}

// gearDescription reads R-17's optional prose for one camera or lens, from
// <library>/_data/gear/<name>.md where <name> is the gear's slugified name --
// "dmc-g6.md", not the tag's "gear-camera-dmc-g6". A gear without a file simply
// has nothing said about it.
func gearDescription(library, name string) string {
	return describedBy(library, "gear", name)
}

// tagDescription reads R-19's optional prose for one tag, from
// <library>/_data/tags/<slug>.md, where <slug> is the tag's own slug -- the one
// in its URL, "category-architecture.md" for Category|Architecture. A tag
// without a file simply has nothing said about it.
func tagDescription(library, slug string) string {
	return describedBy(library, "tags", slug)
}

// describedBy reads one Markdown description out of the library's _data. The
// front matter is dropped: the photographer may have written a title there, but
// the page already has one, and what is wanted is the prose.
func describedBy(library, kind, name string) string {
	raw, err := os.ReadFile(filepath.Join(library, libraryData, kind, name+".md"))
	if err != nil {
		return ""
	}
	_, body := splitFrontMatter(string(raw))
	return strings.TrimSpace(body)
}

// checkGearDescriptions fails the build on an R-17 file naming no gear.
func checkGearDescriptions(library string, known map[string]string) {
	checkDescriptions(library, "gear", known,
		"no camera or lens in the library is named %q")
}

// checkTagDescriptions is R-19's half of the same bargain: a file under
// _data/tags/ that names no tag the library actually carries fails the build.
func checkTagDescriptions(library string, known map[string]bool) {
	named := make(map[string]string, len(known))
	for slug := range known {
		named[slug] = slug
	}
	checkDescriptions(library, "tags", named,
		"no tag in the library has the slug %q")
}

// checkDescriptions fails the build on a description file that names nothing.
// Silently ignoring it would leave the photographer looking at a page that
// never shows what they wrote.
func checkDescriptions(library, kind string, known map[string]string, complaint string) {
	dir := filepath.Join(library, libraryData, kind)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // the library need not describe anything
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		stem := strings.TrimSuffix(name, ".md")
		if _, ok := known[stem]; !ok {
			fail("%s: "+complaint, filepath.Join(dir, name), stem)
		}
	}
}

// reservedSlugs are the URL segments the generated site already owns (§11 Q6).
// A page called "albums" would shadow the album index rather than sit beside it.
var reservedSlugs = map[string]bool{
	"albums": true, "tags": true, "locations": true, "p": true, "img": true,
	"timeline": true, "all": true, "gear": true,
}

var yearSlug = regexp.MustCompile(`^\d{4}$`)

// loadLibraryPages reads R-13 pages: plain Markdown in <library>/pages, rendered
// as standalone pages beside the photographs (F-21).
func loadLibraryPages(library string, existing []page) []page {
	dir := filepath.Join(library, libraryData, "pages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // the library need not carry any
	}

	taken := map[string]string{}
	for _, p := range existing {
		taken[p.path] = p.path
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	pages := make([]page, 0, len(names))
	for _, name := range names {
		slug := slugify(strings.TrimSuffix(name, ".md"))
		if slug == "" {
			fail("%s: filename does not reduce to a usable slug", filepath.Join(dir, name))
		}
		if reservedSlugs[slug] || yearSlug.MatchString(slug) {
			fail("%s: slug %q would shadow a generated URL", filepath.Join(dir, name), slug)
		}
		target := filepath.Join("photos", slug+".md")
		if _, clash := taken[target]; clash {
			fail("%s: slug %q collides with a generated page", filepath.Join(dir, name), slug)
		}
		taken[target] = target

		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fail("cannot read %s: %v", filepath.Join(dir, name), err)
		}
		meta, body := splitFrontMatter(string(raw))
		if meta.Title == "" {
			meta.Title = strings.TrimSuffix(name, ".md")
		}
		pages = append(pages, page{
			path: target, title: meta.Title, layout: "prose",
			params: map[string]any{"slug": slug},
			body:   body,
			menu:   meta.Menu,
			weight: meta.Weight,
		})
	}
	return pages
}

// pageMeta is the front matter an R-13 page may carry.
type pageMeta struct {
	Title  string `yaml:"title"`
	Menu   bool   `yaml:"menu"`   // F-21: show in the site menu
	Weight int    `yaml:"weight"` // menu order; ties fall back to title
}

// splitFrontMatter pulls optional YAML off the top of a page.
func splitFrontMatter(raw string) (pageMeta, string) {
	var meta pageMeta
	body := strings.TrimPrefix(raw, "\ufeff")
	if !strings.HasPrefix(body, "---\n") {
		return meta, body
	}
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		return meta, body
	}
	head := body[4 : 4+end]
	// Step past the closing delimiter's own line, not merely its newline —
	// leaving the "---" behind makes Markdown render a stray horizontal rule.
	rest := body[4+end+1:]
	if cut := strings.Index(rest, "\n"); cut >= 0 {
		rest = rest[cut+1:]
	} else {
		rest = ""
	}
	rest = strings.TrimLeft(rest, "\n")

	if err := yaml.Unmarshal([]byte(head), &meta); err != nil {
		return pageMeta{}, body
	}
	return meta, rest
}

// albumYears is F-7's date on an album: one year when every photograph was taken
// in it, and the span otherwise, since an album shot over three years is not a
// 2024 album. Empty when no member carries a capture date.
func albumYears(album Album) string {
	first, last := deref(album.FirstYear), deref(album.LastYear)
	switch {
	case first == "" || last == "":
		return ""
	case first == last:
		return first
	default:
		return first + "\u2013" + last // en dash: a range, not a hyphenated word
	}
}

func cover(album Album) string {
	if album.Cover == nil {
		return ""
	}
	return *album.Cover
}

// without and only split a list of tag slugs on R-15's derived namespace: what
// the photographer wrote, and what the camera did.
func without(slugs []string, gear map[string]bool) []string {
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		if !gear[slug] {
			out = append(out, slug)
		}
	}
	return out
}

// gearOrder lists the bodies before the glass — the kind level (R-15) says
// which is which — and orders each group by the name a reader sees.
func gearOrder(slugs []string, gear map[string]bool, kinds, titles map[string]string) []string {
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		if gear[slug] {
			out = append(out, slug)
		}
	}
	rank := func(slug string) int {
		switch kinds[slug] {
		case gearCamera:
			return 0
		case gearLens:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if r, s := rank(out[i]), rank(out[j]); r != s {
			return r < s
		}
		// Lenses read in focal order, the way they sit in a bag: widest first.
		// Cameras have no such axis, so they stay alphabetical.
		if kinds[out[i]] == gearLens && kinds[out[j]] == gearLens {
			a, aok := focalLength(titles[out[i]])
			b, bok := focalLength(titles[out[j]])
			if aok != bok {
				return aok // a lens whose name hides its focal length sorts last
			}
			if aok && a != b {
				return a < b
			}
		}
		return titles[out[i]] < titles[out[j]]
	})
	return out
}

// focalMillimetres reads "12-40mm" and "50mm", the form most lens names use.
// focalSlash reads "7-14/F4.0", which is how Panasonic writes the same thing --
// no "mm" at all, the aperture behind a slash.
var (
	focalMillimetres = regexp.MustCompile(`(\d+(?:\.\d+)?)(?:-\d+(?:\.\d+)?)?\s*mm`)
	focalSlash       = regexp.MustCompile(`(\d+(?:\.\d+)?)(?:-\d+(?:\.\d+)?)?\s*/`)
)

// focalLength pulls the focal length out of a lens name, in millimetres. A zoom
// is placed at the wide end of its range, which is where it is used when it is
// chosen over a prime, and which keeps "12-40" beside "12" rather than beside
// "40". The aperture is never a candidate: it is only ever reached through the
// "mm" or the slash that the focal length owns.
func focalLength(title string) (float64, bool) {
	for _, re := range []*regexp.Regexp{focalMillimetres, focalSlash} {
		if m := re.FindStringSubmatch(title); m != nil {
			if mm, err := strconv.ParseFloat(m[1], 64); err == nil {
				return mm, true
			}
		}
	}
	return 0, false
}

// named pairs each slug with the name it is written out under, so a listing can
// link by slug while showing the hierarchy (R-6).
func named(slugs []string, titles map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(slugs))
	for _, slug := range slugs {
		title := titles[slug]
		if title == "" {
			title = slug
		}
		out = append(out, map[string]string{"slug": slug, "title": title})
	}
	return out
}

// reversed copies a slice back to front. The index lists years newest first,
// the way the timeline reads them (F-10), rather than starting a reader at the
// oldest year in the library.
func reversed(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

func sortedMapKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writePage(path string, p page) {
	front := map[string]any{"title": p.title, "params": p.params}
	if p.menu {
		front["menus"] = []string{"main"}
		front["weight"] = p.weight
	}
	if p.date != "" {
		front["date"] = p.date
	}
	if p.layout != "" {
		front["layout"] = p.layout
	}
	data, err := json.MarshalIndent(front, "", "  ")
	if err != nil {
		fail("cannot encode front matter for %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail("cannot create %s: %v", filepath.Dir(path), err)
	}
	data = append(data, '\n')
	if p.body != "" {
		data = append(data, '\n')
		data = append(data, p.body...)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fail("cannot write %s: %v", path, err)
	}
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fail("cannot encode %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail("cannot create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		fail("cannot write %s: %v", path, err)
	}
}

// writeHugoConfig keeps B-4 in one place: the base URL comes from exposer.yaml,
// so nothing has to be passed on the Hugo command line.
func writeHugoConfig(path string, cfg config) {
	body := fmt.Sprintf(`baseURL = %q
title = %q
disableKinds = ["taxonomy", "term", "robotsTXT", "404"]

# F-19 asks for a feed of new albums — one feed, not one per section.
[outputs]
  home = ["HTML", "RSS"]
  section = ["HTML"]
  page = ["HTML"]

[params]
  subtitle = %q
  imprintPage = %q
  imprintLabel = %q
  footerLine = %q
`, cfg.BaseURL, cfg.Title, cfg.Subtitle, cfg.ImprintPage, cfg.ImprintLabel, cfg.FooterLine)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail("cannot create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fail("cannot write %s: %v", path, err)
	}
}

func copyTree(src fs.FS, from, to string) {
	err := fs.WalkDir(src, from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, from), "/")
		target := filepath.Join(to, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		fail("cannot copy %s to %s: %v", from, to, err)
	}
}
