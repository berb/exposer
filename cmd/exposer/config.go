package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type derivativeConfig struct {
	Widths []int `yaml:"widths"`
	// Square crops for the places that need a uniform tile rather than the
	// photograph's own shape (F-7's album covers). Long-edge pixels, as above.
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
	FooterNote   string           `yaml:"footer_note"`
	MinRating    int              `yaml:"min_rating"`
	Derivatives  derivativeConfig `yaml:"derivatives"`
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
		fmt.Fprintf(os.Stderr, "no %s; building with the defaults\n", path)
		return defaultConfig()
	}
	return loadConfig(path)
}

func defaultConfig() config {
	cfg := config{
		BaseURL:      "/",
		Title:        "Photographs",
		ImprintLabel: "Imprint",
		MinRating:    publishMinRating,
		Derivatives: derivativeConfig{
			Widths:       []int{200, 400, 800, 1600, 2400},
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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fail("%s: %v", path, err)
	}
	if len(cfg.Derivatives.Widths) == 0 || len(cfg.Derivatives.Formats) == 0 {
		fail("%s: derivatives need at least one width and one format (F-5)", path)
	}
	return cfg
}
