package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// copyLibrary gives a test a library of its own to change.
func copyLibrary(t *testing.T) string {
	t.Helper()
	library := filepath.Join(t.TempDir(), "library")
	if err := os.CopyFS(library, os.DirFS(goodLibrary)); err != nil {
		t.Fatal(err)
	}
	return library
}

func TestIndexReadsOnlyWhatChanged(t *testing.T) {
	requireTools(t, "exiftool")
	library := copyLibrary(t)
	cache := filepath.Join(t.TempDir(), "exif.json")
	version := exifToolVersion()
	files := len(listLibrary(library))

	cold, n := scanLibrary(library, cache, version)
	if n != files {
		t.Fatalf("a cold scan read %d of %d files", n, files)
	}

	// F-2: nothing changed, so exiftool has nothing to do -- and what the
	// cache hands back is exactly what exiftool said.
	warm, n := scanLibrary(library, cache, version)
	if n != 0 {
		t.Errorf("a warm scan read %d files, want none", n)
	}
	if !reflect.DeepEqual(cold, warm) {
		t.Error("the cached scan differs from the scan it cached")
	}

	// A darktable edit rewrites the sidecar only, and that is all that is read.
	sidecar := filepath.Join(library, "2024", "03", "harbour-dawn.jpg.xmp")
	raw := string(readBytes(t, sidecar))
	edited := strings.Replace(raw, `xmp:Rating="5"`, `xmp:Rating="2"`, 1)
	if edited == raw {
		t.Fatal("harbour-dawn's sidecar has no rating of 5 to change")
	}
	write(t, sidecar, edited)
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(sidecar, later, later); err != nil {
		t.Fatal(err)
	}
	after, n := scanLibrary(library, cache, version)
	if n != 1 {
		t.Errorf("after one sidecar changed, %d files were read", n)
	}
	if got := toString(after[sidecar]["XMP:Rating"]); got != "2" {
		t.Errorf("the edited sidecar reads rating %q, want 2", got)
	}

	// A file that left the library leaves the cache with it.
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	gone, _ := scanLibrary(library, cache, version)
	if _, ok := gone[sidecar]; ok {
		t.Error("a deleted sidecar is still scanned")
	}
	var stored exifCache
	read(t, cache, &stored)
	if _, ok := stored.Files[filepath.Join("2024", "03", "harbour-dawn.jpg.xmp")]; ok {
		t.Error("a deleted sidecar is still in the cache")
	}
}

func TestIndexCacheIsDroppedWhenItCannotBeTrusted(t *testing.T) {
	requireTools(t, "exiftool")
	cache := filepath.Join(t.TempDir(), "exif.json")
	version := exifToolVersion()
	files := len(listLibrary(goodLibrary))
	scanLibrary(goodLibrary, cache, version)

	// Another exiftool may read the same file differently.
	if _, read := scanLibrary(goodLibrary, cache, version+"-other"); read != files {
		t.Errorf("under another exiftool, %d of %d files were read", read, files)
	}

	// A damaged cache costs a full read, never the build.
	write(t, cache, "{ not json")
	if _, read := scanLibrary(goodLibrary, cache, version); read != files {
		t.Errorf("with a damaged cache, %d of %d files were read", read, files)
	}

	// So does one written for other arguments.
	var stored map[string]any
	read(t, cache, &stored)
	stored["args"] = []string{"-json"}
	data, _ := json.Marshal(stored)
	write(t, cache, string(data))
	if _, read := scanLibrary(goodLibrary, cache, version); read != files {
		t.Errorf("with a cache for other arguments, %d of %d files were read", read, files)
	}
}

func TestLibraryListingMatchesExifToolRecursion(t *testing.T) {
	requireTools(t, "exiftool")
	// listLibrary replaced `exiftool -r`, so it must find the same files:
	// extensions in any case, hidden files but not hidden directories, and
	// symlinks followed, to files and to directories.
	root := t.TempDir()
	library := filepath.Join(root, "library")
	jpeg := readBytes(t, filepath.Join(goodLibrary, "2024", "03", "harbour-dawn.jpg"))
	for _, rel := range []string{
		"a/x.jpg", "a/x.jpg.xmp", "a/Y.JPG", "a/z.NEF", "a/notes.txt",
		".hidden/h.jpg", ".dot.jpg", "_data/d.jpg",
	} {
		path := filepath.Join(library, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, jpeg, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(other, "o.jpg"), string(jpeg))
	for link, target := range map[string]string{
		"linked": other, "file.jpg": "a/x.jpg", "dangling.jpg": "nowhere.jpg", "again": "a",
	} {
		if err := os.Symlink(target, filepath.Join(library, link)); err != nil {
			t.Fatal(err)
		}
	}

	for _, lib := range []string{library, goodLibrary} {
		args := []string{"-r", "-q", "-q", "-p", "$Directory/$FileName"}
		for _, ext := range append(append([]string{}, originalExts...), "xmp") {
			args = append(args, "-ext", ext)
		}
		out, _ := exec.Command("exiftool", append(args, lib)...).Output()
		var want []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if rel, err := filepath.Rel(lib, line); err == nil && line != "" {
				want = append(want, rel)
			}
		}
		sort.Strings(want)
		if got := listLibrary(lib); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n  listLibrary found %v\n  exiftool -r found %v", lib, got, want)
		}
	}
}

func TestLibraryListingSurvivesASymlinkLoop(t *testing.T) {
	// Here listLibrary parts from exiftool on purpose: exiftool -r follows a
	// link to an enclosing directory until the path grows too long, listing
	// every photograph again at each level. Each directory is walked once.
	library := t.TempDir()
	write(t, filepath.Join(library, "x.jpg"), "")
	if err := os.Mkdir(filepath.Join(library, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(library, "a", "up")); err != nil {
		t.Fatal(err)
	}
	if got := listLibrary(library); !reflect.DeepEqual(got, []string{"x.jpg"}) {
		t.Errorf("listLibrary found %v, want x.jpg once", got)
	}
}

func TestIndexFromTheCacheIsByteIdentical(t *testing.T) {
	requireTools(t, "exiftool")
	// F-3 through F-2: an index built from cached metadata is the index built
	// from exiftool, byte for byte.
	dir := t.TempDir()
	build := func(out string) []byte {
		runIndex([]string{
			"--library", goodLibrary,
			"--out", filepath.Join(dir, out),
			"--cache", filepath.Join(dir, "hashes.json"),
		})
		return readBytes(t, filepath.Join(dir, out))
	}
	cold := build("cold.json")
	if _, err := os.Stat(filepath.Join(dir, "exif.json")); err != nil {
		t.Fatalf("the metadata cache is not beside the hash cache: %v", err)
	}
	if warm := build("warm.json"); string(warm) != string(cold) {
		t.Error("the index built from the cache differs from the one built from exiftool")
	}
}
