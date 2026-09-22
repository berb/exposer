// Stage 1: read the library, write target/index.json.
//
// Reads originals and their XMP sidecars, never writes to either (R-1).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

const (
	schemaVersion     = 4
	extractorVersion  = 1
	publishMinRating  = 4
	albumNamespace    = "Album"
	locationNamespace = "Location"
	gearNamespace     = "Gear"
	gearCamera        = "Camera"
	gearLens          = "Lens"
)

var originalExts = []string{"jpg", "jpeg", "orf", "arw", "cr2", "cr3", "dng", "nef", "raf", "rw2"}

// R-7: namespaces that exist for the editing workflow and never reach the site.
var workflowNamespaces = map[string]bool{"darktable": true}

var (
	dateTimeRe = regexp.MustCompile(`^(\d{4})[:-](\d{2})[:-](\d{2})[ T](\d{2}):(\d{2}):(\d{2})`)
	numberRe   = regexp.MustCompile(`[-+]?[\d.]+`)
	nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)
	dashesRe   = regexp.MustCompile(`-+`)
)

// pyFloat marshals like Python's json module, which keeps the ".0" on integral
// floats. Without it the two implementations differ on every whole number.
type pyFloat float64

func (f pyFloat) MarshalJSON() ([]byte, error) {
	s := strconv.FormatFloat(float64(f), 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return []byte(s), nil
}

type Tag struct {
	Slug string   `json:"slug"`
	Path []string `json:"path"`
	Leaf string   `json:"leaf"`
}

type Camera struct {
	Make  *string `json:"make"`
	Model *string `json:"model"`
	Lens  *string `json:"lens"`
}

type Exposure struct {
	F       *pyFloat `json:"f"`
	Shutter *string  `json:"shutter"`
	ISO     *int     `json:"iso"`
	Focal   *pyFloat `json:"focal"`
}

type Rights struct {
	Creator *string `json:"creator"`
	Rights  *string `json:"rights"`
}

type FileInfo struct {
	Bytes  int64   `json:"bytes"`
	Width  *int    `json:"width"`
	Height *int    `json:"height"`
	MIME   *string `json:"mime"`
	SHA256 string  `json:"sha256"`
}

type Photo struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	Published  bool     `json:"published"`
	Rating     int      `json:"rating"`
	Label      *string  `json:"label"`
	CapturedAt *string  `json:"captured_at"`
	Title      *string  `json:"title"`
	Caption    *string  `json:"caption"`
	Alt        string   `json:"alt"`
	Tags       []Tag    `json:"tags"`
	Albums     []string `json:"albums"`
	Locations  []string `json:"locations"`
	Camera     Camera   `json:"camera"`
	Exposure   Exposure `json:"exposure"`
	Rights     Rights   `json:"rights"`
	File       FileInfo `json:"file"`

	albumTags     []string
	locationPaths [][]string
	// R-8 asks whether a photograph was given a way to be found. Gear tags are
	// derived from EXIF, not curated, so they are counted separately.
	curatedTags int
}

type Album struct {
	Slug  string  `json:"slug"`
	Tag   string  `json:"tag"`
	Title string  `json:"title"`
	Date  *string `json:"date"`
	Cover *string `json:"cover"`
	// The years the album's published photographs were taken in, earliest and
	// latest. An album is often shot over more than one year, so a single date
	// would describe only its beginning.
	FirstYear  *string `json:"first_year"`
	LastYear   *string `json:"last_year"`
	Intro      *string `json:"intro"`
	PhotoCount int     `json:"photo_count"`
	// F-18: reachable by URL, kept out of indexes, feeds and the sitemap.
	Unlisted bool `json:"unlisted"`
}

type Location struct {
	Slug        string   `json:"slug"`
	Tag         string   `json:"tag"`
	Title       string   `json:"title"`
	Path        []string `json:"path"`
	Lat         *pyFloat `json:"lat"`
	Lon         *pyFloat `json:"lon"`
	Description *string  `json:"description"`
	PhotoCount  int      `json:"photo_count"`
}

type Tooling struct {
	ExifTool  string `json:"exiftool"`
	Extractor int    `json:"extractor"`
}

type Document struct {
	SchemaVersion int        `json:"schema_version"`
	LibraryRoot   string     `json:"library_root"`
	Tooling       Tooling    `json:"tooling"`
	Photos        []Photo    `json:"photos"`
	Albums        []Album    `json:"albums"`
	Locations     []Location `json:"locations"`
	// R-16: the photographer's own selection, in the order they wrote it.
	Featured []string `json:"featured"`
}

type cacheEntry struct {
	Size   int64   `json:"size"`
	MTime  float64 `json:"mtime"`
	SHA256 string  `json:"sha256"`
}

type albumMeta struct {
	Tag      string `yaml:"tag"`
	Title    string `yaml:"title"`
	Date     string `yaml:"date"`
	Cover    string `yaml:"cover"`
	Intro    string `yaml:"intro"`
	Unlisted bool   `yaml:"unlisted"`
}

type locationMeta struct {
	Tag         string   `yaml:"tag"`
	Title       string   `yaml:"title"`
	Lat         *float64 `yaml:"lat"`
	Lon         *float64 `yaml:"lon"`
	Description string   `yaml:"description"`
}

// fail ends the build. It is a variable rather than a function so a test can
// reach the paths that use it: R-8's contract violations, R-12's conflicting
// duplicates, an unknown path in featured_photos.yaml, a gear description that
// names nothing. Those paths exist to stop a bad build, and a test that cannot
// call them leaves the loudest part of this program unexercised. Nothing but a
// test may replace it.
var fail = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func slugify(text string) string {
	var ascii strings.Builder
	for _, r := range norm.NFKD.String(text) {
		if r < 128 { // mirrors Python's .encode("ascii", "ignore")
			ascii.WriteRune(r)
		}
	}
	s := nonAlnumRe.ReplaceAllString(strings.ToLower(ascii.String()), "-")
	return strings.Trim(dashesRe.ReplaceAllString(s, "-"), "-")
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := toString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := toString(v); s != "" {
			return []string{s}
		}
		return nil
	}
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return "" // arrays, objects and nil have no scalar form
	}
}

// pick returns the first non-empty value, mirroring the Python first().
func pick(sources ...any) *string {
	for _, v := range sources {
		if s := toString(v); s != "" {
			return &s
		}
	}
	return nil
}

func lookup(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

func parseNumber(v any) *float64 {
	s := toString(v)
	if s == "" {
		return nil
	}
	match := numberRe.FindString(strings.ReplaceAll(s, "+", ""))
	if match == "" {
		return nil
	}
	f, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return nil
	}
	return &f
}

func parseDateTime(v *string) *string {
	if v == nil {
		return nil
	}
	m := dateTimeRe.FindStringSubmatch(*v)
	if m == nil {
		return nil
	}
	out := fmt.Sprintf("%s-%s-%sT%s:%s:%s", m[1], m[2], m[3], m[4], m[5], m[6])
	return &out
}

func compareSlices(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return len(a) - len(b)
}

func exifToolVersion() string {
	out, err := exec.Command("exiftool", "-ver").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func fileDigest(path, rel string, cache map[string]cacheEntry) string {
	info, err := os.Stat(path)
	if err != nil {
		fail("cannot stat %s: %v", path, err)
	}
	mtime := float64(info.ModTime().UnixNano()) / 1e9
	if hit, ok := cache[rel]; ok && hit.Size == info.Size() && hit.MTime == mtime {
		return hit.SHA256
	}
	fh, err := os.Open(path)
	if err != nil {
		fail("cannot read %s: %v", path, err)
	}
	defer fh.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, fh); err != nil {
		fail("cannot hash %s: %v", path, err)
	}
	sum := hex.EncodeToString(digest.Sum(nil))
	cache[rel] = cacheEntry{Size: info.Size(), MTime: mtime, SHA256: sum}
	return sum
}

func classifyTags(hierarchical []string) (albums []string, locations [][]string, display [][]string) {
	seenAlbum := map[string]bool{}
	seenLocation := map[string]bool{}
	seenDisplay := map[string]bool{}

	for _, raw := range hierarchical {
		var parts []string
		for _, part := range strings.Split(raw, "|") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		if len(parts) == 0 {
			continue
		}
		key := strings.Join(parts, "\x00")
		switch {
		case parts[0] == albumNamespace && len(parts) > 1:
			name := strings.Join(parts[1:], "|")
			if !seenAlbum[name] {
				seenAlbum[name] = true
				albums = append(albums, name)
			}
		case parts[0] == locationNamespace && len(parts) > 1:
			if !seenLocation[key] {
				seenLocation[key] = true
				locations = append(locations, parts[1:])
			}
		case len(parts) == 1 || workflowNamespaces[parts[0]]:
			// Workflow tags (R-7): never displayed.
		default:
			if !seenDisplay[key] {
				seenDisplay[key] = true
				display = append(display, parts)
			}
		}
	}
	sort.Strings(albums)
	sort.Slice(locations, func(i, j int) bool { return compareSlices(locations[i], locations[j]) < 0 })
	sort.Slice(display, func(i, j int) bool { return compareSlices(display[i], display[j]) < 0 })
	return albums, locations, display
}

// buildDimensions: RAW files carry no File:ImageWidth; Composite:ImageSize is
// present for both.
// gearKind is the level between Gear and the equipment itself: a camera body
// and a lens are different kinds of thing, and saying so lets a listing put the
// bodies before the glass without guessing from the name.
type gearKind struct {
	kind  string
	value *string
}

// withGearTags appends Gear|Camera|<model> and Gear|Lens|<model>, keeping the
// display tags deduplicated and sorted so the index stays deterministic (F-3).
func withGearTags(display [][]string, values ...gearKind) [][]string {
	seen := map[string]bool{}
	for _, path := range display {
		seen[strings.Join(path, "\x00")] = true
	}
	for _, value := range values {
		name := strings.TrimSpace(deref(value.value))
		if name == "" {
			continue
		}
		path := []string{gearNamespace, value.kind, name}
		key := strings.Join(path, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		display = append(display, path)
	}
	sort.Slice(display, func(i, j int) bool { return compareSlices(display[i], display[j]) < 0 })
	return display
}

func buildDimensions(image map[string]any) (*int, *int) {
	if size := toString(lookup(image, "Composite:ImageSize")); size != "" {
		parts := strings.FieldsFunc(size, func(r rune) bool { return r == 'x' || r == ' ' })
		if len(parts) == 2 {
			w, errW := strconv.Atoi(parts[0])
			h, errH := strconv.Atoi(parts[1])
			if errW == nil && errH == nil {
				return &w, &h
			}
		}
	}
	width := toInt(parseNumber(pickAny(lookup(image, "File:ImageWidth"),
		lookup(image, "EXIF:ImageWidth"), lookup(image, "EXIF:ExifImageWidth"))))
	height := toInt(parseNumber(pickAny(lookup(image, "File:ImageHeight"),
		lookup(image, "EXIF:ImageHeight"), lookup(image, "EXIF:ExifImageHeight"))))
	return width, height
}

func pickAny(sources ...any) any {
	for _, v := range sources {
		if toString(v) != "" {
			return v
		}
	}
	return nil
}

func buildPhoto(source string, image, sidecar map[string]any, library string,
	cache map[string]cacheEntry, minRating int) Photo {

	rel, err := filepath.Rel(library, source)
	if err != nil {
		fail("cannot relativise %s: %v", source, err)
	}

	rating := 0
	if r := parseNumber(lookup(sidecar, "XMP:Rating")); r != nil {
		rating = int(*r)
	}
	albums, locations, display := classifyTags(asStrings(lookup(sidecar, "XMP:HierarchicalSubject")))
	curated := len(display)
	// R-15: the camera and lens are already in the metadata contract (R-4);
	// tagging them costs nothing and turns "everything shot with this lens"
	// into a page.
	display = withGearTags(display,
		gearKind{gearCamera, pick(lookup(image, "EXIF:Model"))},
		gearKind{gearLens, pick(lookup(image, "EXIF:LensModel"))})

	sum := fileDigest(source, rel, cache)

	tags := make([]Tag, 0, len(display))
	for _, path := range display {
		tags = append(tags, Tag{
			Slug: slugify(strings.Join(path, "-")),
			Path: path,
			Leaf: path[len(path)-1],
		})
	}
	albumSlugs := make([]string, 0, len(albums))
	for _, name := range albums {
		albumSlugs = append(albumSlugs, slugify(name))
	}
	locationSlugs := make([]string, 0, len(locations))
	for _, path := range locations {
		locationSlugs = append(locationSlugs, slugify(strings.Join(path, "-")))
	}

	info, err := os.Stat(source)
	if err != nil {
		fail("cannot stat %s: %v", source, err)
	}

	width, height := buildDimensions(image)
	title := pick(lookup(sidecar, "XMP:Title"))
	caption := pick(lookup(sidecar, "XMP:Description"))
	// D-9 wants non-empty alt text; the id is the last resort when the library
	// offers nothing to say about the photo.
	alt := sum[:16]
	if caption != nil {
		alt = *caption
	} else if title != nil {
		alt = *title
	}

	var creator *string
	if list := asStrings(lookup(sidecar, "XMP:Creator")); len(list) > 0 {
		creator = &list[0]
	} else {
		creator = pick(lookup(image, "EXIF:Artist"))
	}

	return Photo{
		ID:        sum[:16],
		Source:    rel,
		Published: rating >= minRating,
		Rating:    rating,
		Label:     pick(lookup(sidecar, "XMP:Label")),
		CapturedAt: parseDateTime(pick(lookup(sidecar, "XMP:DateTimeOriginal"),
			lookup(image, "EXIF:DateTimeOriginal"),
			lookup(sidecar, "XMP:CreateDate"),
			lookup(image, "EXIF:CreateDate"))),
		Title:     title,
		Caption:   caption,
		Alt:       alt,
		Tags:      tags,
		Albums:    albumSlugs,
		Locations: locationSlugs,
		Camera: Camera{
			Make:  pick(lookup(image, "EXIF:Make")),
			Model: pick(lookup(image, "EXIF:Model")),
			Lens:  pick(lookup(image, "EXIF:LensModel")),
		},
		Exposure: Exposure{
			F:       toPyFloat(parseNumber(lookup(image, "EXIF:FNumber"))),
			Shutter: pick(lookup(image, "EXIF:ExposureTime")),
			ISO:     toInt(parseNumber(lookup(image, "EXIF:ISO"))),
			Focal:   toPyFloat(parseNumber(lookup(image, "EXIF:FocalLength"))),
		},
		Rights: Rights{
			Creator: creator,
			Rights: pick(lookup(sidecar, "XMP:Rights"),
				lookup(image, "EXIF:Copyright")),
		},
		File: FileInfo{
			Bytes:  info.Size(),
			Width:  width,
			Height: height,
			MIME:   pick(lookup(image, "File:MIMEType")),
			SHA256: sum,
		},
		albumTags:     albums,
		locationPaths: locations,
		curatedTags:   curated,
	}
}

func toPyFloat(v *float64) *pyFloat {
	if v == nil {
		return nil
	}
	f := pyFloat(*v)
	return &f
}

func toInt(v *float64) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

// validateContract implements R-8 plus D-9.
func validateContract(photo Photo) []string {
	var problems []string
	if photo.CapturedAt == nil {
		problems = append(problems, "no capture date")
	}
	// Any of the three gives the photo a page beyond the timeline, which is what
	// R-8 and acceptance criterion 4 are actually asking for.
	// Gear tags deliberately do not count: nearly every photograph has a camera,
	// so letting them satisfy this would make the check vacuous. R-8 is asking
	// whether the photograph was curated into something findable.
	if photo.curatedTags == 0 && len(photo.Albums) == 0 && len(photo.Locations) == 0 {
		problems = append(problems, "no tags, album or location")
	}
	return problems
}

// libraryData is where the library keeps the files that describe the site
// rather than a photograph: albums (R-3), locations (R-10) and pages (R-13).
// One folder keeps them out of the way of the photographs, which may sit in
// any directory the photographer likes (R-9).
const libraryData = "_data"

// featuredFile is R-16's list: library-relative paths to the photographs the
// photographer wants shown first. A list of paths, or the same list under a
// "photos:" key, whichever the file happens to use.
const featuredFile = "featured_photos.yaml"

// loadFeatured resolves those paths to ids. A path that names nothing, or names
// a photograph the gate holds back (R-5), fails the build: silently dropping a
// photograph the photographer asked for by name is exactly the kind of quiet
// skipping R-8 rules out.
func loadFeatured(library string, photos []Photo) []string {
	path := filepath.Join(library, libraryData, featuredFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		fail("cannot read %s: %v", path, err)
	}

	var listed []string
	if err := yaml.Unmarshal(data, &listed); err != nil {
		var wrapped struct {
			Photos []string `yaml:"photos"`
		}
		if err := yaml.Unmarshal(data, &wrapped); err != nil {
			fail("%s: %v", path, err)
		}
		listed = wrapped.Photos
	}

	bySource := make(map[string]*Photo, len(photos))
	for i := range photos {
		bySource[filepath.ToSlash(photos[i].Source)] = &photos[i]
	}

	featured := make([]string, 0, len(listed))
	seen := map[string]bool{}
	for _, entry := range listed {
		rel := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(entry, "./")))
		if rel == "" {
			continue
		}
		photo, ok := bySource[rel]
		if !ok {
			fail("%s: '%s' is not a photo in the library", path, entry)
		}
		if !photo.Published {
			fail("%s: '%s' is featured but does not pass the publication gate (rating %d)",
				path, entry, photo.Rating)
		}
		if seen[photo.ID] {
			continue
		}
		seen[photo.ID] = true
		featured = append(featured, photo.ID)
	}
	return featured
}

func readYAMLDir(dir string, into func(path string, data []byte)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yaml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			fail("cannot read %s: %v", path, err)
		}
		into(path, data)
	}
}

func loadAlbums(library string, photos []Photo) []Album {
	byTag := map[string][]*Photo{}
	for i := range photos {
		for _, tag := range photos[i].albumTags {
			byTag[tag] = append(byTag[tag], &photos[i])
		}
	}

	declared := map[string]albumMeta{}
	declaredFile := map[string]string{}
	readYAMLDir(filepath.Join(library, libraryData, "albums"), func(path string, data []byte) {
		var meta albumMeta
		if err := yaml.Unmarshal(data, &meta); err != nil {
			fail("%s: %v", path, err)
		}
		parts := strings.SplitN(meta.Tag, "|", 2)
		if len(parts) != 2 {
			fail("%s: 'tag' must look like 'Album|<name>'", path)
		}
		declared[parts[1]] = meta
		declaredFile[parts[1]] = path
	})

	var unknown []string
	for name := range declared {
		if _, ok := byTag[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		fail("album file matches no photo: %s", strings.Join(unknown, ", "))
	}

	names := make([]string, 0, len(byTag))
	for name := range byTag {
		names = append(names, name)
	}
	sort.Strings(names)

	albums := make([]Album, 0, len(names))
	for _, name := range names {
		meta, hasMeta := declared[name]
		members := byTag[name]
		sort.SliceStable(members, func(i, j int) bool {
			a, b := members[i], members[j]
			ac, bc := deref(a.CapturedAt), deref(b.CapturedAt)
			if ac != bc {
				return ac < bc
			}
			return a.ID < b.ID
		})
		var published []*Photo
		for _, p := range members {
			if p.Published {
				published = append(published, p)
			}
		}

		slug := slugify(name)
		if hasMeta {
			slug = strings.TrimSuffix(filepath.Base(declaredFile[name]), ".yaml")
		}

		var cover *string
		if hasMeta && meta.Cover != "" {
			var matches []*Photo
			for _, p := range members {
				if filepath.Base(p.Source) == meta.Cover {
					matches = append(matches, p)
				}
			}
			if len(matches) != 1 {
				fail("%s: cover '%s' matches %d photos in this album",
					declaredFile[name], meta.Cover, len(matches))
			}
			// Copy: photos is re-sorted later, which would move this value.
			id := matches[0].ID
			cover = &id
		} else if len(published) > 0 {
			id := published[0].ID
			cover = &id
		}

		// published is sorted by capture time, so the span is its ends — but a
		// photograph may carry no capture date at all, and R-8 only fails the
		// build for a published one, so scan rather than index.
		var firstYear, lastYear *string
		for _, p := range published {
			if p.CapturedAt == nil {
				continue
			}
			year := (*p.CapturedAt)[:4]
			if firstYear == nil || year < *firstYear {
				y := year
				firstYear = &y
			}
			if lastYear == nil || year > *lastYear {
				y := year
				lastYear = &y
			}
		}

		var date *string
		if hasMeta && meta.Date != "" {
			d := meta.Date
			date = &d
		} else if len(published) > 0 && published[0].CapturedAt != nil {
			d := (*published[0].CapturedAt)[:10]
			date = &d
		}

		title := name
		if hasMeta && meta.Title != "" {
			title = meta.Title
		}
		var intro *string
		if trimmed := strings.TrimSpace(meta.Intro); trimmed != "" {
			intro = &trimmed
		}

		albums = append(albums, Album{
			Slug:       slug,
			Tag:        albumNamespace + "|" + name,
			Title:      title,
			Date:       date,
			Cover:      cover,
			FirstYear:  firstYear,
			LastYear:   lastYear,
			Intro:      intro,
			PhotoCount: len(published),
			Unlisted:   hasMeta && meta.Unlisted,
		})
	}
	return albums
}

// loadLocations implements R-10: every node of the Location hierarchy.
func loadLocations(library string, photos []Photo) []Location {
	nodes := map[string][]string{}
	for _, photo := range photos {
		for _, path := range photo.locationPaths {
			for depth := 1; depth <= len(path); depth++ {
				node := path[:depth]
				nodes[strings.Join(node, "\x00")] = node
			}
		}
	}

	declared := map[string]locationMeta{}
	readYAMLDir(filepath.Join(library, libraryData, "locations"), func(path string, data []byte) {
		var meta locationMeta
		if err := yaml.Unmarshal(data, &meta); err != nil {
			fail("%s: %v", path, err)
		}
		parts := strings.Split(meta.Tag, "|")
		if len(parts) < 2 || parts[0] != locationNamespace {
			fail("%s: 'tag' must look like 'Location|<place>'", path)
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		declared[strings.Join(parts[1:], "\x00")] = meta
	})

	var unknown []string
	for key := range declared {
		if _, ok := nodes[key]; !ok {
			unknown = append(unknown, strings.ReplaceAll(key, "\x00", "|"))
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		fail("location file matches no photo: %s", strings.Join(unknown, ", "))
	}

	keys := make([][]string, 0, len(nodes))
	for _, node := range nodes {
		keys = append(keys, node)
	}
	sort.Slice(keys, func(i, j int) bool { return compareSlices(keys[i], keys[j]) < 0 })

	locations := make([]Location, 0, len(keys))
	seenSlugs := map[string]string{}
	for _, node := range keys {
		key := strings.Join(node, "\x00")
		meta := declared[key]
		slug := slugify(strings.Join(node, "-"))
		if previous, clash := seenSlugs[slug]; clash {
			fail("location slug '%s' is claimed by both %s and %s",
				slug, previous, strings.Join(node, "|"))
		}
		seenSlugs[slug] = strings.Join(node, "|")

		// A photo tagged Germany|Ulm counts for Germany too (R-6).
		count := 0
		for _, photo := range photos {
			if !photo.Published {
				continue
			}
			for _, path := range photo.locationPaths {
				if len(path) >= len(node) && compareSlices(path[:len(node)], node) == 0 {
					count++
					break
				}
			}
		}

		title := node[len(node)-1]
		if meta.Title != "" {
			title = meta.Title
		}
		var description *string
		if trimmed := strings.TrimSpace(meta.Description); trimmed != "" {
			description = &trimmed
		}

		locations = append(locations, Location{
			Slug:        slug,
			Tag:         locationNamespace + "|" + strings.Join(node, "|"),
			Title:       title,
			Path:        node,
			Lat:         toPyFloat(meta.Lat),
			Lon:         toPyFloat(meta.Lon),
			Description: description,
			PhotoCount:  count,
		})
	}
	return locations
}

// dedupeOriginals collapses byte-identical originals imported more than once.
//
// The id is a content hash (§11 Q7), so two copies of one photograph claim the
// same id and the same URL — they are the same photograph, not a collision. The
// canonical copy is the lexicographically first path, which keeps the index
// deterministic (F-3) regardless of scan order.
//
// Divergent curation is a different matter: same pixels, two different sets of
// ratings or tags is a conflict only darktable can settle, so it fails loudly
// rather than picking a winner (R-8's principle: never skip silently).
func dedupeOriginals(photos []Photo) ([]Photo, int) {
	bySum := map[string][]Photo{}
	var order []string
	for _, photo := range photos {
		if _, seen := bySum[photo.File.SHA256]; !seen {
			order = append(order, photo.File.SHA256)
		}
		bySum[photo.File.SHA256] = append(bySum[photo.File.SHA256], photo)
	}

	var kept []Photo
	var conflicts []string
	collapsed := 0
	for _, sum := range order {
		group := bySum[sum]
		sort.Slice(group, func(i, j int) bool { return group[i].Source < group[j].Source })
		if len(group) == 1 {
			kept = append(kept, group[0])
			continue
		}
		reference := curationFingerprint(group[0])
		for _, other := range group[1:] {
			if curationFingerprint(other) != reference {
				conflicts = append(conflicts, fmt.Sprintf(
					"  %s and %s are the same file but carry different metadata",
					group[0].Source, other.Source))
			}
		}
		kept = append(kept, group[0])
		collapsed += len(group) - 1
		for _, other := range group[1:] {
			detail("%s  same original as %s, which is kept", other.Source, group[0].Source)
		}
	}

	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		fail("%d duplicate original(s) disagree about their metadata:\n%s",
			len(conflicts), strings.Join(conflicts, "\n"))
	}
	return kept, collapsed
}

func curationFingerprint(photo Photo) string {
	subset := struct {
		Rating                 int
		Label, Captured        *string
		Title, Caption         *string
		Tags                   []Tag
		Albums, Locations      []string
		AlbumTags, LocationSet any
	}{
		photo.Rating, photo.Label, photo.CapturedAt, photo.Title, photo.Caption,
		photo.Tags, photo.Albums, photo.Locations, photo.albumTags, photo.locationPaths,
	}
	data, err := json.Marshal(subset)
	if err != nil {
		fail("cannot fingerprint %s: %v", photo.Source, err)
	}
	return string(data)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func runIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	library := fs.String("library", "source", "library root (read-only)")
	out := fs.String("out", filepath.Join("target", "index.json"), "index output path")
	cachePath := fs.String("cache", filepath.Join("target", "cache", "hashes-go.json"), "hash cache path")
	exifCachePath := fs.String("exif-cache", "", "metadata cache path (default: exif.json beside --cache)")
	configPath := fs.String("config", "", "generator config (default: <library>/_data/exposer.yaml)")
	minRating := fs.Int("min-rating", publishMinRating, "publication gate (default: the config's min_rating)")
	v, q := addOutputFlags(fs)
	fs.Parse(args)
	applyOutputFlags(v, q)

	// R-5's gate lives in the library's configuration, since it describes that
	// library's star discipline rather than this build. The flag still wins,
	// for trying a different gate without editing anything.
	explicitRating := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "min-rating" {
			explicitRating = true
		}
	})
	if !explicitRating {
		*minRating = configFor(*library, *configPath).MinRating
	}

	if info, err := os.Stat(*library); err != nil || !info.IsDir() {
		fail("library not found: %s", *library)
	}

	if *exifCachePath == "" {
		*exifCachePath = filepath.Join(filepath.Dir(*cachePath), "exif.json")
	}
	exifTool := exifToolVersion()
	scanned, _ := scanLibrary(*library, *exifCachePath, exifTool)

	cache := map[string]cacheEntry{}
	if data, err := os.ReadFile(*cachePath); err == nil {
		_ = json.Unmarshal(data, &cache)
	}

	sources := make([]string, 0, len(scanned))
	for source := range scanned {
		if !strings.HasSuffix(strings.ToLower(source), ".xmp") {
			sources = append(sources, source)
		}
	}
	sort.Strings(sources)

	photos := make([]Photo, 0, len(sources))
	type failure struct {
		source   string
		problems []string
	}
	var failures []failure
	unedited := 0

	for _, source := range sources {
		sidecarPath := source + ".xmp"
		sidecar := scanned[sidecarPath]
		photo := buildPhoto(source, scanned[source], sidecar, *library, cache, *minRating)
		photos = append(photos, photo)

		// A sidecar that exists but yields no XMP is corrupt, and would otherwise
		// drop the photo below the gate without a word (R-8: no silent skipping).
		if _, err := os.Stat(sidecarPath); err == nil {
			readable := false
			for key := range sidecar {
				if strings.HasPrefix(key, "XMP:") {
					readable = true
					break
				}
			}
			if !readable {
				reason := "no XMP metadata found"
				if warning := toString(lookup(sidecar, "ExifTool:Warning")); warning != "" {
					reason = warning
				}
				failures = append(failures, failure{photo.Source,
					[]string{fmt.Sprintf("unreadable sidecar (%s)", reason)}})
			}
		} else {
			unedited++
			detail("%s  not published: no sidecar", photo.Source)
		}
		if _, err := os.Stat(sidecarPath); err == nil && !photo.Published {
			detail("%s  not published: rating %d is below %d", photo.Source, photo.Rating, *minRating)
		}

		if photo.Published {
			if problems := validateContract(photo); len(problems) > 0 {
				failures = append(failures, failure{photo.Source, problems})
			}
		}
	}

	if len(failures) > 0 {
		sort.Slice(failures, func(i, j int) bool { return failures[i].source < failures[j].source })
		lines := make([]string, 0, len(failures))
		for _, f := range failures {
			lines = append(lines, fmt.Sprintf("  %s: %s", f.source, strings.Join(f.problems, ", ")))
		}
		fail("%d photo(s) violate the metadata contract:\n%s",
			len(failures), strings.Join(lines, "\n"))
	}

	photos, collapsed := dedupeOriginals(photos)

	byID := map[string]string{}
	for _, photo := range photos {
		if previous, clash := byID[photo.ID]; clash {
			fail("id collision: %s and %s", photo.Source, previous)
		}
		byID[photo.ID] = photo.Source
	}

	albums := loadAlbums(*library, photos)
	locations := loadLocations(*library, photos)

	sort.SliceStable(photos, func(i, j int) bool {
		a, b := photos[i], photos[j]
		if ac, bc := deref(a.CapturedAt), deref(b.CapturedAt); ac != bc {
			return ac < bc
		}
		return a.ID < b.ID
	})

	document := Document{
		SchemaVersion: schemaVersion,
		LibraryRoot:   *library,
		Tooling:       Tooling{ExifTool: exifTool, Extractor: extractorVersion},
		Photos:        photos,
		Albums:        albums,
		Locations:     locations,
		Featured:      loadFeatured(*library, photos),
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(document); err != nil {
		fail("cannot encode index: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fail("cannot create output directory: %v", err)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		fail("cannot write %s: %v", *out, err)
	}

	if err := os.MkdirAll(filepath.Dir(*cachePath), 0o755); err != nil {
		fail("cannot create cache directory: %v", err)
	}
	cacheJSON, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		fail("cannot encode cache: %v", err)
	}
	if err := os.WriteFile(*cachePath, append(cacheJSON, '\n'), 0o644); err != nil {
		fail("cannot write %s: %v", *cachePath, err)
	}

	published, mapped := 0, 0
	for _, photo := range photos {
		if photo.Published {
			published++
		}
	}
	for _, location := range locations {
		if location.Lat != nil {
			mapped++
		}
	}
	note := ""
	if unedited > 0 {
		note = fmt.Sprintf(", %d without a sidecar", unedited)
	}
	if collapsed > 0 {
		note += fmt.Sprintf(", %d duplicate original(s) collapsed", collapsed)
	}
	report("%d photos (%d published%s), %d albums, %d locations (%d mappable) -> %s",
		len(photos), published, note, len(albums), len(locations), mapped, shown(*out))
}
