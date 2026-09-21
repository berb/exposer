package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berb/exposer"
)

func TestInitWritesTheTemplateIntoALibrary(t *testing.T) {
	library := t.TempDir() // no _data/ yet: init creates it
	if message := catchFail(t, func() { runInit([]string{library}) }); message != "" {
		t.Fatalf("init failed on a fresh library: %s", message)
	}
	written, err := os.ReadFile(filepath.Join(library, libraryData, configFileName))
	if err != nil {
		t.Fatalf("no configuration after init: %v", err)
	}
	if !bytes.Equal(written, exposer.ExampleConfig) {
		t.Errorf("init wrote something other than exposer.example.yaml")
	}
}

func TestTemplateIsTheDefaults(t *testing.T) {
	// R-18. The defaults live twice -- in defaultConfig and in the template --
	// and the template promises it "changes nothing until you edit it". This
	// is that promise, checked: change a default in one place and not the
	// other, and this fails.
	path := filepath.Join(t.TempDir(), configFileName)
	if err := os.WriteFile(path, exposer.ExampleConfig, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := loadConfig(path), defaultConfig(); !reflect.DeepEqual(got, want) {
		t.Errorf("the template does not load as the defaults\n got  %+v\n want %+v", got, want)
	}
}

func TestInitNeverReplacesAConfiguration(t *testing.T) {
	library := t.TempDir()
	path := filepath.Join(library, libraryData, configFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := []byte("title: \"Mine\"\n")
	if err := os.WriteFile(path, mine, 0o644); err != nil {
		t.Fatal(err)
	}

	message := catchFail(t, func() { runInit([]string{library}) })
	if !strings.Contains(message, "already exists") {
		t.Errorf("init over an existing config said %q, want a refusal", message)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, mine) {
		t.Errorf("init changed an existing configuration to %q", got)
	}
}

func TestInitRefusesAMissingLibrary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "photos-typo")
	message := catchFail(t, func() { runInit([]string{missing}) })
	if !strings.Contains(message, "not a directory") {
		t.Errorf("init on a missing library said %q", message)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Errorf("init created %s instead of failing", missing)
	}
}
