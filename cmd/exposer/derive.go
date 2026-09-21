package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type derivative struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Cache  string `json:"cache"`  // relative to the cache root, never deployed
	Public string `json:"public"` // path inside the site tree
	Bytes  int64  `json:"bytes"`
	// A square derivative is centre-cropped, so it does not belong in the
	// srcset of the photograph itself — only where a uniform tile is wanted.
	Square bool `json:"square"`
}

type derivativeManifest struct {
	ParamHash string                  `json:"param_hash"`
	Tooling   map[string]string       `json:"tooling"`
	Photos    map[string][]derivative `json:"photos"`
	// D-7: the average colour, painted under each image so the page never
	// reflows while photos decode. One hex string, no extra request.
	Tones map[string]string `json:"tones"`
}

var formatExt = map[string]string{"jpeg": "jpg", "avif": "avif"}

func toolVersion(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		fail("%s is required but did not run: %v", name, err)
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// paramHash makes the cache self-invalidating. Tool versions belong in it:
// without them a darktable or ImageMagick upgrade silently serves derivatives
// rendered by the old toolchain (F-2, F-3).
func paramHash(cfg config, tooling map[string]string) string {
	h := sha256.New()
	fmt.Fprintf(h, "widths=%v squares=%v formats=%v jq=%d aq=%d",
		cfg.Derivatives.Widths, cfg.Derivatives.SquareWidths, cfg.Derivatives.Formats,
		cfg.Derivatives.JPEGQuality, cfg.Derivatives.AVIFQuality)
	for _, key := range sortedKeys(tooling) {
		fmt.Fprintf(h, " %s=%s", key, tooling[key])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func loadDocument(path string) Document {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("cannot read index %s: %v (run `make index` first)", path, err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		fail("cannot parse %s: %v", path, err)
	}
	return doc
}

func isRaw(source string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(source)), ".")
	return ext != "jpg" && ext != "jpeg"
}

func largest(values []int) int {
	out := 0
	for _, v := range values {
		out = max(out, v)
	}
	return out
}

// ladder returns the long-edge sizes worth rendering: never upscale, and skip a
// size that would duplicate the native one.
func ladder(widths []int, longEdge int) []int {
	var sizes []int
	for _, w := range widths {
		if w <= longEdge {
			sizes = append(sizes, w)
		}
	}
	if len(sizes) == 0 && longEdge > 0 {
		sizes = []int{longEdge}
	}
	return sizes
}

func runDerive(args []string) {
	fs := flag.NewFlagSet("derive", flag.ExitOnError)
	indexPath := fs.String("index", filepath.Join("target", "index.json"), "index to read")
	library := fs.String("library", "source", "library root (read-only)")
	cacheRoot := fs.String("cache", filepath.Join("target", "cache", "derivatives"), "derivative cache root")
	manifestPath := fs.String("manifest", filepath.Join("target", "derivatives.json"), "manifest output")
	configPath := fs.String("config", "", "generator config (default: <library>/_data/exposer.yaml)")
	jobs := fs.Int("jobs", max(1, runtime.NumCPU()/2), "photos rendered concurrently")
	fs.Parse(args)

	cfg := configFor(*library, *configPath)
	doc := loadDocument(*indexPath)

	var published []Photo
	needsDarktable := false
	for _, photo := range doc.Photos {
		if photo.Published {
			published = append(published, photo)
			needsDarktable = needsDarktable || isRaw(photo.Source)
		}
	}

	tooling := map[string]string{
		"imagemagick": toolVersion("magick", "--version"),
		"exiftool":    toolVersion("exiftool", "-ver"),
	}
	// F-4: darktable develops RAW originals and nothing else, so a library of
	// JPEGs builds without it. Its version joins the cache key only when it can
	// affect a derivative -- asking for it regardless made darktable a
	// requirement of every library, and an upgrade of it re-render JPEGs it
	// never touched.
	if needsDarktable {
		tooling["darktable"] = toolVersion("darktable-cli", "--version")
	}
	hash := paramHash(cfg, tooling)

	scratch, err := os.MkdirTemp("", "exposer-derive-")
	if err != nil {
		fail("cannot create scratch dir: %v", err)
	}
	defer os.RemoveAll(scratch)

	var (
		mu       sync.Mutex
		results  = map[string][]derivative{}
		tones    = map[string]string{}
		rendered int
		reused   int
		failures []string
	)

	started := time.Now()
	sem := make(chan int, *jobs)
	var wg sync.WaitGroup

	for i := range published {
		wg.Add(1)
		go func(photo Photo) {
			defer wg.Done()
			slot := <-sem
			defer func() { sem <- slot }()

			derivs, tone, fresh, err := derivePhoto(photo, *library, *cacheRoot, hash, cfg,
				filepath.Join(scratch, fmt.Sprintf("w%d", slot)))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, fmt.Sprintf("  %s: %v", photo.Source, err))
				return
			}
			results[photo.ID] = derivs
			tones[photo.ID] = tone
			if fresh {
				rendered++
			} else {
				reused++
			}
		}(published[i])
	}
	for slot := 0; slot < *jobs; slot++ {
		sem <- slot
	}
	wg.Wait()

	if len(failures) > 0 {
		sort.Strings(failures)
		fmt.Fprintf(os.Stderr, "%d photo(s) failed to render:\n%s\n",
			len(failures), strings.Join(failures, "\n"))
		os.Exit(1)
	}

	manifest := derivativeManifest{ParamHash: hash, Tooling: tooling, Photos: results, Tones: tones}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fail("cannot encode manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*manifestPath), 0o755); err != nil {
		fail("cannot create manifest directory: %v", err)
	}
	if err := os.WriteFile(*manifestPath, append(data, '\n'), 0o644); err != nil {
		fail("cannot write %s: %v", *manifestPath, err)
	}

	count := 0
	for _, derivs := range results {
		count += len(derivs)
	}
	fmt.Printf("%d photos (%d rendered, %d cached), %d derivatives in %s -> %s\n",
		len(published), rendered, reused, count,
		time.Since(started).Round(time.Millisecond), *manifestPath)
}

// derivePhoto renders one photo's ladder, or reports the cached one untouched.
func derivePhoto(photo Photo, library, cacheRoot, hash string, cfg config, scratch string) ([]derivative, string, bool, error) {
	if photo.File.Width == nil || photo.File.Height == nil {
		return nil, "", false, fmt.Errorf("no dimensions in the index")
	}
	longEdge := max(*photo.File.Width, *photo.File.Height)
	sizes := ladder(cfg.Derivatives.Widths, longEdge)
	// A square crop is bounded by the short edge, not the long one: cropping to
	// 400 from a 600x300 photograph would have to upscale.
	shortEdge := min(*photo.File.Width, *photo.File.Height)
	squares := ladder(cfg.Derivatives.SquareWidths, shortEdge)

	source := filepath.Join(library, photo.Source)
	// The photo id is the hash of the original, which a darktable edit never
	// touches — the edit lives in the sidecar. Without it in the key, re-editing
	// a RAW would silently keep serving the previous rendering (F-2).
	renderKey := hash
	if isRaw(source) {
		sidecarSum, err := hashFile(source + ".xmp")
		if err != nil {
			return nil, "", false, fmt.Errorf("hashing sidecar: %w", err)
		}
		sum := sha256.Sum256([]byte(hash + sidecarSum))
		renderKey = hex.EncodeToString(sum[:])[:12]
	}
	dir := filepath.Join(cacheRoot, photo.ID[:2], photo.ID, renderKey)

	wanted := make([]derivative, 0, (len(sizes)+len(squares))*len(cfg.Derivatives.Formats))
	complete := true
	for _, format := range cfg.Derivatives.Formats {
		ext, ok := formatExt[format]
		if !ok {
			return nil, "", false, fmt.Errorf("unknown format %q", format)
		}
		add := func(size int, square bool) {
			name := fmt.Sprintf("%d.%s", size, ext)
			if square {
				name = fmt.Sprintf("%dsq.%s", size, ext)
			}
			d := derivative{
				Width:  size,
				Format: format,
				Cache:  filepath.Join(photo.ID[:2], photo.ID, renderKey, name),
				Public: filepath.Join("photos", "img", photo.ID, name),
				Square: square,
			}
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Size() > 0 {
				d.Bytes = info.Size()
			} else {
				complete = false
			}
			wanted = append(wanted, d)
		}
		for _, size := range sizes {
			add(size, false)
		}
		for _, size := range squares {
			add(size, true)
		}
	}

	metaPath := filepath.Join(dir, "meta.json")
	var meta struct {
		Dims map[string][2]int `json:"dims"`
		Tone string            `json:"tone"`
	}
	if complete {
		if data, err := os.ReadFile(metaPath); err == nil &&
			json.Unmarshal(data, &meta) == nil && len(meta.Dims) == len(wanted) &&
			meta.Tone != "" {
			for i := range wanted {
				if wh, ok := meta.Dims[filepath.Base(wanted[i].Cache)]; ok {
					wanted[i].Width, wanted[i].Height = wh[0], wh[1]
				}
			}
			return wanted, meta.Tone, false, nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", false, err
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return nil, "", false, err
	}

	base := source
	if isRaw(source) {
		base = filepath.Join(scratch, "base.jpg")
		os.Remove(base)
		if err := developRaw(source, base, largest(cfg.Derivatives.Widths), scratch); err != nil {
			return nil, "", false, err
		}
	}

	dims := map[string][2]int{}
	for i := range wanted {
		name := filepath.Base(wanted[i].Cache)
		out := filepath.Join(dir, name)
		quality := cfg.Derivatives.JPEGQuality
		if wanted[i].Format == "avif" {
			quality = cfg.Derivatives.AVIFQuality
		}
		encoder := encode
		if wanted[i].Square {
			encoder = encodeSquare
		}
		w, h, err := encoder(base, out, wanted[i].Width, quality)
		if err != nil {
			return nil, "", false, fmt.Errorf("encoding %s: %w", name, err)
		}
		info, err := os.Stat(out)
		if err != nil {
			return nil, "", false, err
		}
		wanted[i].Width, wanted[i].Height, wanted[i].Bytes = w, h, info.Size()
		dims[name] = [2]int{w, h}
	}

	tone, err := averageTone(base)
	if err != nil {
		return nil, "", false, err
	}

	if err := stampRights(photo, dir, dims); err != nil {
		return nil, "", false, err
	}
	// Re-stat: exiftool rewrites the files, changing their size.
	for i := range wanted {
		if info, err := os.Stat(filepath.Join(dir, filepath.Base(wanted[i].Cache))); err == nil {
			wanted[i].Bytes = info.Size()
		}
	}

	meta.Dims, meta.Tone = dims, tone
	data, err := json.Marshal(meta)
	if err != nil {
		return nil, "", false, err
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		return nil, "", false, err
	}
	return wanted, tone, true, nil
}

// developRaw reproduces the darktable edit headlessly (F-4). The core flags keep
// it away from the user's darktable database — the library is read-only (R-1).
func developRaw(source, out string, maxWidth int, scratch string) error {
	config := filepath.Join(scratch, "dt")
	cmd := exec.Command("darktable-cli",
		source, source+".xmp", out,
		"--width", strconv.Itoa(maxWidth), "--height", strconv.Itoa(maxWidth),
		"--hq", "true", "--apply-custom-presets", "false",
		"--core", "--configdir", config, "--cachedir", config, "--library", ":memory:")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("darktable-cli: %v: %s", err, lastLine(string(output)))
	}
	if info, err := os.Stat(out); err != nil || info.Size() == 0 {
		return fmt.Errorf("darktable-cli produced no output")
	}
	return nil
}

// encode resizes to a long-edge box, never upscaling, and strips every tag (F-16).
func encode(base, out string, size, quality int) (int, int, error) {
	box := fmt.Sprintf("%dx%d>", size, size)
	cmd := exec.Command("magick", base, "-resize", box,
		"-quality", strconv.Itoa(quality), "-strip",
		"-write", out, "-format", "%w %h", "info:")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("magick: %v: %s", err, lastLine(string(output)))
	}
	var w, h int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d %d", &w, &h); err != nil {
		return 0, 0, fmt.Errorf("magick reported %q", strings.TrimSpace(string(output)))
	}
	return w, h, nil
}

// encodeSquare is F-7's 1:1 tile: fill a square box from the centre and crop
// whatever the photograph's own shape leaves over. Never upscales, because the
// caller bounds the size by the short edge.
func encodeSquare(base, out string, size, quality int) (int, int, error) {
	box := fmt.Sprintf("%dx%d", size, size)
	cmd := exec.Command("magick", base, "-resize", box+"^",
		"-gravity", "center", "-extent", box,
		"-quality", strconv.Itoa(quality), "-strip",
		"-write", out, "-format", "%w %h", "info:")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("magick: %v: %s", err, lastLine(string(output)))
	}
	var w, h int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d %d", &w, &h); err != nil {
		return 0, 0, fmt.Errorf("magick reported %q", strings.TrimSpace(string(output)))
	}
	return w, h, nil
}

// averageTone is D-7's flat placeholder: the image reduced to a single pixel.
func averageTone(base string) (string, error) {
	out, err := exec.Command("magick", base, "-resize", "1x1!",
		"-format", "%[hex:p{0,0}]", "info:").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("magick: %v: %s", err, lastLine(string(out)))
	}
	hex := strings.TrimSpace(string(out))
	if len(hex) < 6 {
		return "", fmt.Errorf("magick reported tone %q", hex)
	}
	return "#" + strings.ToLower(hex[:6]), nil
}

// stampRights puts creator and rights back after -strip removed everything (F-16).
// One exiftool call per photo rather than per derivative: spawning dominates.
func stampRights(photo Photo, dir string, dims map[string][2]int) error {
	creator, rights := deref(photo.Rights.Creator), deref(photo.Rights.Rights)
	if creator == "" && rights == "" {
		return nil
	}
	args := []string{"-overwrite_original", "-q", "-m"}
	if creator != "" {
		args = append(args, "-Artist="+creator, "-XMP-dc:Creator="+creator)
	}
	if rights != "" {
		args = append(args, "-Copyright="+rights, "-XMP-dc:Rights="+rights)
	}
	for name := range dims {
		args = append(args, filepath.Join(dir, name))
	}
	sort.Strings(args[len(args)-len(dims):])
	if output, err := exec.Command("exiftool", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("exiftool: %v: %s", err, lastLine(string(output)))
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
