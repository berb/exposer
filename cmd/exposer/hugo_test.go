package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// B-8's download path, against a server of the test's own, so none of this
// touches the network or the real cache.

// fakeHugoRelease serves one tarball at whatever path is asked for and counts
// the requests, and points the fetcher and its cache at test-owned places.
func fakeHugoRelease(t *testing.T, archive []byte) (requests *int, cache string) {
	t.Helper()
	requests = new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		w.Write(archive)
	}))
	t.Cleanup(server.Close)

	cache = t.TempDir()
	originalURL, originalCache := hugoBaseURL, hugoCacheDir
	hugoBaseURL = server.URL
	hugoCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { hugoBaseURL, hugoCacheDir = originalURL, originalCache })
	return requests, cache
}

func tarball(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write(body)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestHugoDownloadRefusesAnArchiveThatIsNotThePinnedOne(t *testing.T) {
	archive := tarball(t, "hugo", []byte("#!/bin/sh\necho not the pinned hugo\n"))
	_, cache := fakeHugoRelease(t, archive)

	message := catchFail(t, func() {
		downloadHugo("linux/amd64", strings.Repeat("0", 64), cache, filepath.Join(cache, "hugo"))
	})
	if !strings.Contains(message, "pinned checksum") {
		t.Fatalf("a wrong archive gave %q, want a checksum failure", message)
	}
	// Nothing may be left behind for the next build to pick up as a cache hit.
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("a refused download left %d file(s) in the cache", len(entries))
	}
}

func TestHugoDownloadInstallsWhatItVerified(t *testing.T) {
	body := []byte("#!/bin/sh\necho hugo\n")
	archive := tarball(t, "hugo", body)
	sum := sha256.Sum256(archive)
	_, cache := fakeHugoRelease(t, archive)

	binary := filepath.Join(cache, "hugo")
	downloadHugo("linux/amd64", hex.EncodeToString(sum[:]), cache, binary)

	got, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("no binary after a verified download: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("installed %q, want the archive's hugo", got)
	}
	if info, _ := os.Stat(binary); info.Mode()&0o111 == 0 {
		t.Errorf("installed hugo is not executable: %v", info.Mode())
	}
	// The temp file is renamed into place, never left beside it.
	if matches, _ := filepath.Glob(filepath.Join(cache, "*.partial")); len(matches) != 0 {
		t.Errorf("left %v behind", matches)
	}
}

func TestCachedHugoIsUsedWithoutTheNetwork(t *testing.T) {
	if _, ok := hugoArchives[runtime.GOOS+"/"+runtime.GOARCH]; !ok {
		t.Skip("this platform takes Hugo from PATH, not from the cache")
	}
	requests, cache := fakeHugoRelease(t, nil)
	cached := filepath.Join(cache, "hugo")
	if err := os.WriteFile(cached, []byte("cached"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findHugo(""); got != cached {
		t.Errorf("findHugo = %s, want the cached %s", got, cached)
	}
	if *requests != 0 {
		t.Errorf("a cache hit made %d request(s)", *requests)
	}
}

func TestHugoOfAnotherVersionIsRefused(t *testing.T) {
	// --hugo and a Hugo on PATH are checked the same way: a renderer that is
	// not the pinned one breaks F-3 without any other symptom.
	fake := filepath.Join(t.TempDir(), "hugo")
	script := "#!/bin/sh\necho 'hugo v0.99.0-deadbeef linux/amd64 BuildDate=unknown'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	message := catchFail(t, func() { findHugo(fake) })
	if !strings.Contains(message, hugoVersion) || !strings.Contains(message, "v0.99.0") {
		t.Errorf("an old hugo gave %q, want both versions named", message)
	}
}
