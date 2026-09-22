package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type derivativeConfig struct {
	Widths []int `yaml:"widths"`
	// Square crops for the places that need a uniform tile rather than the
	// photograph's own shape (F-5). Long-edge pixels, as above.
	SquareWidths []int    `yaml:"square_widths"`
	Formats      []string `yaml:"formats"`
	JPEGQuality  int      `yaml:"jpeg_quality"`
	AVIFQuality  int      `yaml:"avif_quality"`
}

type config struct {
	BaseURL      string           `yaml:"base_url"`
	Title        string           `yaml:"title"`
	Subtitle     string           `yaml:"subtitle"`
	ImprintPage  string           `yaml:"imprint_page"`
	ImprintLabel string           `yaml:"imprint_label"`
	FooterLine   string           `yaml:"footer_line"`
	MinRating    int              `yaml:"min_rating"`
	Derivatives  derivativeConfig `yaml:"derivatives"`
}

// onceFor runs say the first time it is asked about key in this process. Every
// stage of a build loads the configuration, and a notice worth reading once is
// noise the third time.
func onceFor(key string, say func()) {
	outputMu.Lock()
	seen := announced[key]
	announced[key] = true
	outputMu.Unlock()
	if !seen {
		say()
	}
}

var announced = map[string]bool{}

// renamedSettings maps a setting's old name to its new one, so a configuration
// written for an earlier release fails with the fix rather than a bare
// "field not found".
var renamedSettings = map[string]string{
	"footer_note": "footer_line",
}

// configFileName is where a library keeps the generator's configuration: with
// the rest of what the library says about itself (R-18), rather than beside the
// generator, which knows nothing about any particular site.
const configFileName = "exposer.yaml"

// configFor resolves the configuration a stage should read. An explicit
// --config wins, and must exist; otherwise the library's own file is used, and
// a library without one builds on the defaults rather than refusing to build.
func configFor(library, override string) config {
	if override != "" {
		return loadConfig(override)
	}
	path := filepath.Join(library, libraryData, configFileName)
	if _, err := os.Stat(path); err != nil {
		onceFor(path, func() { notice("no %s; building with the defaults", shown(path)) })
		return defaultConfig()
	}
	onceFor(path, func() { detail("configuration: %s", shown(path)) })
	return loadConfig(path)
}

func defaultConfig() config {
	cfg := config{
		BaseURL:      "/",
		Title:        "Photographs",
		ImprintLabel: "Imprint",
		MinRating:    publishMinRating,
		Derivatives: derivativeConfig{
			Widths:       []int{200, 400, 800, 1200, 1600, 2400},
			SquareWidths: []int{200, 400},
			Formats:      []string{"jpeg", "avif"},
			JPEGQuality:  82,
			AVIFQuality:  50,
		},
	}
	return cfg
}

func loadConfig(path string) config {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		fail("cannot read config %s: %v", path, err)
	}
	// Strict: a key exposer does not know fails the build rather than being
	// ignored, so a typo -- base_ulr -- or a renamed setting cannot silently
	// leave the site built on a default.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		for old, renamed := range renamedSettings {
			if bytes.Contains(data, []byte(old+":")) {
				fail("%s: %s was renamed to %s", path, old, renamed)
			}
		}
		fail("%s: %v", path, err)
	}
	if len(cfg.Derivatives.Widths) == 0 || len(cfg.Derivatives.Formats) == 0 {
		fail("%s: derivatives need at least one width and one format", path)
	}
	return cfg
}
