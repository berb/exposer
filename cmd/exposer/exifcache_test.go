package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestExifToolSplitReadsWhatOneProcessReads(t *testing.T) {
	requireTools(t, "exiftool")
	// Enough files for several processes, whose results must be exactly one
	// process's: the split is for speed and may change nothing else.
	library := t.TempDir()
	jpeg := readBytes(t, filepath.Join(goodLibrary, "2024", "03", "harbour-dawn.jpg"))
	var rels []string
	for i := range 100 {
		rel := filepath.Join(fmt.Sprintf("d%d", i%7), fmt.Sprintf("p%03d.jpg", i))
		if err := os.MkdirAll(filepath.Join(library, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(library, rel), jpeg, 0o644); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, rel)
	}

	split := runExifTool(library, rels)
	entries, err := exifToolBatch(library, rels)
	if err != nil {
		t.Fatal(err)
	}
	if len(split) != len(rels) || len(entries) != len(rels) {
		t.Fatalf("read %d files split and %d in one process, want %d", len(split), len(entries), len(rels))
	}
	for _, entry := range entries {
		source := toString(entry["SourceFile"])
		// Reading a file moves its access time, so the second read of each
		// may see what the first left; nothing in the index uses it.
		delete(entry, "File:FileAccessDate")
		delete(split[source], "File:FileAccessDate")
		if !reflect.DeepEqual(split[source], entry) {
			t.Errorf("%s reads differently when the files are split", source)
		}
	}
}

// captureNotices returns what notice() said while body ran.
func captureNotices(t *testing.T, body func()) string {
	t.Helper()
	var buf bytes.Buffer
	outputMu.Lock()
	original := stderr
	stderr = &buf
	outputMu.Unlock()
	defer func() {
		outputMu.Lock()
		stderr = original
		outputMu.Unlock()
	}()
	body()
	return buf.String()
}

func TestIndexDoesNotRememberAFileExifToolCouldNotRead(t *testing.T) {
	requireTools(t, "exiftool")
	if os.Geteuid() == 0 {
		t.Skip("root reads a file with no permissions, so there is nothing to fail")
	}
	library := copyLibrary(t)
	cache := filepath.Join(t.TempDir(), "exif.json")
	version := exifToolVersion()

	// Still being copied in, or unreadable to this user: either way exiftool
	// reports the failure on the file's own entry and exits non-zero.
	rel := filepath.Join("inbox", "unreadable.jpg")
	path := filepath.Join(library, rel)
	write(t, path, string(readBytes(t, filepath.Join(library, "2024", "03", "harbour-dawn.jpg"))))
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}

	var first map[string]map[string]any
	said := captureNotices(t, func() { first, _ = scanLibrary(library, cache, version) })
	if !strings.Contains(said, path) || !strings.Contains(said, "Error opening file") {
		t.Errorf("the failure was not reported with the file and the reason, got %q", said)
	}
	var stored exifCache
	read(t, cache, &stored)
	if _, ok := stored.Files[rel]; ok {
		t.Error("a file exiftool could not read was written to the cache")
	}
	if toString(first[path]["EXIF:Model"]) != "" {
		t.Error("an unreadable file has metadata")
	}

	// The cause is fixed without touching the file's size or mtime, which is
	// what a chmod does. A remembered failure would never be read again.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	second, read := scanLibrary(library, cache, version)
	if read != 1 {
		t.Errorf("after the permissions were fixed, %d files were read, want just the one", read)
	}
	if toString(second[path]["EXIF:Model"]) == "" {
		t.Errorf("the file reads no camera model once it can be opened: %v", second[path])
	}
}

func TestIndexReadsAgainAFailureAnOlderCacheRemembered(t *testing.T) {
	requireTools(t, "exiftool")
	library := copyLibrary(t)
	cache := filepath.Join(t.TempDir(), "exif.json")
	version := exifToolVersion()
	path := filepath.Join(library, "2024", "03", "harbour-dawn.jpg")
	rel := filepath.Join("2024", "03", "harbour-dawn.jpg")

	// Earlier releases cached a failure under the file's size and mtime.
	scanLibrary(library, cache, version)
	var stored exifCache
	read(t, cache, &stored)
	record := stored.Files[rel]
	record.Tags = map[string]any{"ExifTool:Error": "Error opening file"}
	stored.Files[rel] = record
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	write(t, cache, string(data))

	scanned, n := scanLibrary(library, cache, version)
	if n != 1 {
		t.Errorf("%d files were read, want the one whose cached entry was a failure", n)
	}
	if toString(scanned[path]["EXIF:Model"]) == "" {
		t.Error("the file still reads as the failure the cache held")
	}
}
