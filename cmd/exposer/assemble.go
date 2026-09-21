package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// runAssemble builds the deploy artifact (B-5): Hugo's HTML plus the cached
// derivatives, hardlinked so nothing is copied twice.
func runAssemble(args []string) {
	set := flag.NewFlagSet("assemble", flag.ExitOnError)
	public := set.String("public", filepath.Join("target", "hugo", "public"), "hugo output")
	manifestPath := set.String("manifest", filepath.Join("target", "derivatives.json"), "derivative manifest")
	cacheRoot := set.String("cache", filepath.Join("target", "cache", "derivatives"), "derivative cache root")
	indexPath := set.String("index", filepath.Join("target", "index.json"), "index, for the F-15 check")
	out := set.String("out", filepath.Join("target", "site"), "deploy artifact")
	set.Parse(args)

	manifestData, err := os.ReadFile(*manifestPath)
	if err != nil {
		fail("cannot read %s: %v", *manifestPath, err)
	}
	var manifest derivativeManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		fail("cannot parse %s: %v", *manifestPath, err)
	}

	if err := os.RemoveAll(*out); err != nil {
		fail("cannot clear %s: %v", *out, err)
	}

	pages := linkTree(*public, *out)

	derivatives := 0
	for _, derivs := range manifest.Photos {
		for _, d := range derivs {
			from := filepath.Join(*cacheRoot, d.Cache)
			to := filepath.Join(*out, d.Public)
			if err := linkFile(from, to); err != nil {
				fail("cannot place %s: %v", d.Public, err)
			}
			derivatives++
		}
	}

	leaked := checkNoOriginals(*out, *indexPath)
	if len(leaked) > 0 {
		fail("F-15 violation: %d original(s) reached the deploy artifact:\n  %s",
			len(leaked), strings.Join(leaked, "\n  "))
	}

	unreachable := checkReachable(*out, *indexPath)
	if len(unreachable) > 0 {
		fail("criterion 4 violation: %d published photograph(s) are not reachable:\n  %s",
			len(unreachable), strings.Join(unreachable, "\n  "))
	}

	fmt.Printf("%d pages, %d derivatives -> %s (no originals, F-15; all reachable)\n",
		pages, derivatives, *out)
}

func linkTree(from, to string) int {
	count := 0
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		count++
		return linkFile(path, filepath.Join(to, rel))
	})
	if err != nil {
		fail("cannot assemble %s: %v", from, err)
	}
	return count
}

// linkFile hardlinks, falling back to a copy across filesystems.
func linkFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	os.Remove(to)
	if err := os.Link(from, to); err == nil {
		return nil
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// checkNoOriginals enforces F-15. Hashing the whole tree would be wasteful, so
// only files whose size matches an original are hashed — a copied original
// necessarily matches on size first.
func checkNoOriginals(root, indexPath string) []string {
	doc := loadDocument(indexPath)
	bySize := map[int64]map[string]string{}
	for _, photo := range doc.Photos {
		if bySize[photo.File.Bytes] == nil {
			bySize[photo.File.Bytes] = map[string]string{}
		}
		bySize[photo.File.Bytes][photo.File.SHA256] = photo.Source
	}

	var leaked []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		candidates, ok := bySize[info.Size()]
		if !ok {
			return nil
		}
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		if source, hit := candidates[sum]; hit {
			rel, _ := filepath.Rel(root, path)
			leaked = append(leaked, fmt.Sprintf("%s (is %s)", rel, source))
		}
		return nil
	})
	if err != nil {
		fail("cannot check %s: %v", root, err)
	}
	sort.Strings(leaked)
	return leaked
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// §12 criterion 4: every published photograph is reachable from the timeline
// and from at least one other listing. R-8 already asks the metadata whether a
// photograph deserves such a page; this asks the artifact whether the page
// actually links to it, which is a different question and the one a template
// regression breaks. A listing that silently drops its photographs passes every
// check upstream of here.
//
// Only listing pages count. A scoped page (F-11a) links to its neighbours, so
// counting those would let a photograph vouch for the one beside it. The All
// page (F-23) and the gear tags (R-15) are excluded for the reason R-8 excludes
// gear: they hold every photograph, so letting them answer would make the check
// vacuous.
func checkReachable(root, indexPath string) []string {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		fail("cannot read %s: %v", indexPath, err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		fail("cannot parse %s: %v", indexPath, err)
	}

	timeline := map[string]bool{}
	other := map[string]bool{}
	link := regexp.MustCompile(`/photos/(?:[^"']*/)?p/([0-9a-f]{6,})/|/photos/[^"']*?/([0-9a-f]{16})/`)

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "index.html" {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		kind := listingKind(filepath.ToSlash(rel))
		if kind == "" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen := timeline
		if kind == "other" {
			seen = other
		}
		for _, m := range link.FindAllStringSubmatch(string(body), -1) {
			for _, id := range m[1:] {
				if id != "" {
					seen[id] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		fail("cannot read %s: %v", root, err)
	}

	var unreachable []string
	for _, photo := range doc.Photos {
		if !photo.Published {
			continue
		}
		switch {
		case !timeline[photo.ID]:
			unreachable = append(unreachable, photo.Source+" — not on the timeline")
		case !other[photo.ID]:
			unreachable = append(unreachable, photo.Source+" — only on the timeline")
		}
	}
	return unreachable
}

// listingKind says whether a directory is a listing that counts towards
// reachability, and which half of the criterion it answers. "" means it counts
// for neither.
func listingKind(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "photos" {
		return ""
	}
	switch parts[1] {
	case "timeline":
		return "timeline"
	case "albums", "locations":
		// photos/albums/<slug> is the listing; photos/albums/<slug>/<id> is a
		// scoped page and does not vouch for its neighbours.
		if len(parts) == 3 {
			return "other"
		}
	case "tags":
		if len(parts) == 3 && !strings.HasPrefix(parts[2], "gear-") {
			return "other"
		}
	case "all", "p", "img", "gear":
		return ""
	default:
		// photos/<year> and photos/<year>/<month> are the timeline's own pages.
		if yearSlug.MatchString(parts[1]) && len(parts) <= 3 {
			return "timeline"
		}
	}
	return ""
}
