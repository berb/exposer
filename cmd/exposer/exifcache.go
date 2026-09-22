package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// exifArgs is what stage 1 asks exiftool for. Changing it changes what a
// cached entry holds, so it is part of the cache's key.
var exifArgs = []string{"-json", "-G0", "-a", "-struct", "-c", "%+.7f"}

// exifCache makes stage 1 incremental (F-2). Reading metadata was nearly the
// whole of a warm build -- exiftool reads every original in full, one after
// another -- though nothing had changed. An entry is trusted while its file
// keeps its size and mtime, the same test the photo-id cache applies, and the
// whole cache is dropped when exiftool, the extractor or the arguments change.
type exifCache struct {
	ExifTool  string                `json:"exiftool"`
	Extractor int                   `json:"extractor"`
	Args      []string              `json:"args"`
	Files     map[string]exifRecord `json:"files"` // keyed by path relative to the library
}

type exifRecord struct {
	Size  int64          `json:"size"`
	MTime float64        `json:"mtime"`
	Tags  map[string]any `json:"tags"`
}

// loadExifCache returns the cache at path, or an empty one when it is missing,
// unreadable or keyed on another toolchain. A cache is never worth failing a
// build over: at worst the build reads everything again.
func loadExifCache(path, version string) exifCache {
	fresh := exifCache{ExifTool: version, Extractor: extractorVersion, Args: exifArgs,
		Files: map[string]exifRecord{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return fresh
	}
	var cached exifCache
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // as runExifTool decodes, so a cached number is the number read
	if dec.Decode(&cached) != nil || cached.ExifTool != version ||
		cached.Extractor != extractorVersion ||
		strings.Join(cached.Args, "\x00") != strings.Join(exifArgs, "\x00") ||
		cached.Files == nil {
		return fresh
	}
	return cached
}

// scanLibrary returns exiftool's view of every original and sidecar, keyed by
// the library joined with each file's relative path, and how many files it had
// to read. Files whose size and mtime match the cache are not read again; the
// cache is left holding exactly the files the library holds now.
func scanLibrary(library, cachePath, version string) (map[string]map[string]any, int) {
	cache := loadExifCache(cachePath, version)

	rels := listLibrary(library)
	current := make(map[string]exifRecord, len(rels))
	var misses []string
	for _, rel := range rels {
		info, err := os.Stat(filepath.Join(library, rel))
		if err != nil {
			fail("cannot stat %s: %v", filepath.Join(library, rel), err)
		}
		record := exifRecord{Size: info.Size(), MTime: float64(info.ModTime().UnixNano()) / 1e9}
		if hit, ok := cache.Files[rel]; ok && hit.Size == record.Size && hit.MTime == record.MTime {
			record.Tags = hit.Tags
		} else {
			misses = append(misses, rel)
		}
		current[rel] = record
	}

	read := runExifTool(library, misses)
	for _, rel := range misses {
		record := current[rel]
		record.Tags = read[filepath.Join(library, rel)]
		current[rel] = record
	}
	detail("exiftool read %d of %d files; the rest were unchanged", len(misses), len(rels))

	scanned := make(map[string]map[string]any, len(current))
	for rel, record := range current {
		if record.Tags != nil {
			scanned[filepath.Join(library, rel)] = record.Tags
		}
	}

	cache.Files = current
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		fail("cannot create cache directory: %v", err)
	}
	data, err := json.Marshal(cache)
	if err != nil {
		fail("cannot encode %s: %v", cachePath, err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		fail("cannot write %s: %v", cachePath, err)
	}
	return scanned, len(misses)
}

// listLibrary finds what `exiftool -r -ext <each original> -ext xmp` would:
// extensions in any case, hidden files but not hidden directories, symlinked
// files and symlinked directories alike, except a loop. Paths are relative and
// sorted.
func listLibrary(library string) []string {
	wanted := map[string]bool{".xmp": true}
	for _, ext := range originalExts {
		wanted["."+ext] = true
	}

	var out []string
	// Only the directories above this one: a link to a sibling is walked again,
	// as exiftool walks it, and R-12 collapses what it finds twice. A link to
	// an ancestor is not, where exiftool would recurse until the path is too
	// long to open.
	ancestors := map[string]bool{}
	var walk func(rel string)
	walk = func(rel string) {
		dir := filepath.Join(library, rel)
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			if ancestors[real] {
				return
			}
			ancestors[real] = true
			defer delete(ancestors, real)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			fail("cannot read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			child := filepath.Join(rel, name)
			isDir := entry.IsDir()
			if entry.Type()&fs.ModeSymlink != 0 {
				info, err := os.Stat(filepath.Join(library, child))
				if errors.Is(err, fs.ErrNotExist) {
					continue // a dangling link names nothing to read
				} else if err != nil {
					fail("cannot stat %s: %v", filepath.Join(library, child), err)
				}
				isDir = info.IsDir()
			}
			switch {
			case isDir && !strings.HasPrefix(name, "."):
				walk(child)
			case !isDir && wanted[strings.ToLower(filepath.Ext(name))]:
				out = append(out, child)
			}
		}
	}
	walk("")
	sort.Strings(out)
	return out
}

// runExifTool reads the given files, relative to the library. A first build
// reads everything, and exiftool reads one file at a time, so the files are
// split across processes -- half the cores, as stage 2 renders, and never
// fewer than minPerProcess files each, since every process pays for starting
// Perl. The results are keyed by file, so the split cannot change them.
func runExifTool(library string, rels []string) map[string]map[string]any {
	const minPerProcess = 32
	processes := max(1, min(runtime.NumCPU()/2, len(rels)/minPerProcess))

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		scanned  = make(map[string]map[string]any, len(rels))
		failures []string
	)
	for i := range processes {
		// Every processes-th file, so that one directory of large RAWs is not
		// all one process's share.
		var share []string
		for j := i; j < len(rels); j += processes {
			share = append(share, rels[j])
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries, err := exifToolBatch(library, share)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err.Error())
				return
			}
			for _, entry := range entries {
				scanned[toString(entry["SourceFile"])] = entry
			}
		}()
	}
	wg.Wait()
	if len(failures) > 0 {
		sort.Strings(failures)
		fail("%s", strings.Join(failures, "\n"))
	}
	return scanned
}

// exifToolBatch reads files in one exiftool process. The names go in on stdin
// (-@ -), since a large library's would not fit on a command line; each line
// is one name, and every name begins with the library's path, so none can be
// mistaken for an option.
func exifToolBatch(library string, rels []string) ([]map[string]any, error) {
	if len(rels) == 0 {
		return nil, nil
	}
	var names bytes.Buffer
	for _, rel := range rels {
		names.WriteString(filepath.Join(library, rel))
		names.WriteByte('\n')
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("exiftool", append(append([]string{}, exifArgs...), "-@", "-")...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = &names, &stdout, &stderr
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		return nil, fmt.Errorf("exiftool failed: %s", strings.TrimSpace(stderr.String()))
	}

	var entries []map[string]any
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.UseNumber()
	if stdout.Len() > 0 {
		if err := dec.Decode(&entries); err != nil {
			return nil, fmt.Errorf("cannot parse exiftool output: %v", err)
		}
	}
	return entries, nil
}
