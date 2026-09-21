package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// B-8: Hugo is pinned, because F-3's byte-identical rebuild only holds while
// the renderer does not change underneath it. The pin lives here, with the
// hash of every archive this binary is willing to run, so a download is checked
// against what was compiled in rather than against a checksum file fetched from
// the same server as the archive.
const hugoVersion = "0.166.0"

// hugoArchives are the plain (not extended) release archives by platform. The
// theme uses no SCSS and no image processing -- stage 2 renders every
// derivative -- so extended would be a bigger download for nothing.
//
// Hugo publishes macOS only as an installer package, which is not an archive
// Go's standard library can open, so macOS takes the PATH route below instead.
var hugoArchives = map[string]string{
	"linux/amd64": "45228f5a52eb118b0ca168068f01d7df0447314a24056f1d29667ed9fc368308",
	"linux/arm64": "0e15cbc595e799401698c11af39d2594b64292b39d9ec7a19665bd43e05fcb2c",
}

// hugoBaseURL is a variable so tests can point it at a server of their own.
var hugoBaseURL = "https://github.com/gohugoio/hugo/releases/download"

// hugoCacheDir is where downloaded Hugo lives; a variable for the same reason.
var hugoCacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "exposer", "hugo", hugoVersion), nil
}

// findHugo returns the Hugo binary a build renders with, in this order: the one
// named by --hugo; the cached download; a fresh download, verified; and, on a
// platform with no archive to download, a hugo on PATH that reports exactly the
// pinned version. Anything else fails the build rather than rendering with a
// Hugo whose output nobody has checked.
func findHugo(override string) string {
	if override != "" {
		requireHugoVersion(override)
		return override
	}

	platform := runtime.GOOS + "/" + runtime.GOARCH
	sum, downloadable := hugoArchives[platform]
	if !downloadable {
		onPath, err := exec.LookPath("hugo")
		if err != nil {
			fail("no Hugo %s download for %s: install Hugo %s and put it on PATH, or pass --hugo <path>",
				hugoVersion, platform, hugoVersion)
		}
		requireHugoVersion(onPath)
		return onPath
	}

	dir, err := hugoCacheDir()
	if err != nil {
		fail("cannot find a cache directory for Hugo: %v (pass --hugo <path>)", err)
	}
	binary := filepath.Join(dir, "hugo")
	if _, err := os.Stat(binary); err == nil {
		return binary
	}
	downloadHugo(platform, sum, dir, binary)
	return binary
}

// downloadHugo fetches the pinned archive, checks it against the compiled-in
// hash, and moves the binary into place with a rename, so two builds starting
// at once cannot leave a half-written Hugo for the second to run.
func downloadHugo(platform, want, dir, binary string) {
	asset := fmt.Sprintf("hugo_%s_%s.tar.gz", hugoVersion, strings.ReplaceAll(platform, "/", "-"))
	url := fmt.Sprintf("%s/v%s/%s", hugoBaseURL, hugoVersion, asset)
	fmt.Fprintf(os.Stderr, "fetching Hugo %s for %s (once; cached in %s)\n", hugoVersion, platform, dir)

	resp, err := http.Get(url)
	if err != nil {
		fail("cannot download %s: %v (pass --hugo <path> to build offline)", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail("cannot download %s: %s", url, resp.Status)
	}
	archive, err := io.ReadAll(resp.Body)
	if err != nil {
		fail("cannot download %s: %v", url, err)
	}

	digest := sha256.Sum256(archive)
	if got := hex.EncodeToString(digest[:]); got != want {
		fail("%s does not match its pinned checksum (B-8)\n  want %s\n  got  %s", asset, want, got)
	}

	body, err := hugoFromArchive(archive)
	if err != nil {
		fail("cannot unpack %s: %v", asset, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail("cannot create %s: %v", dir, err)
	}
	partial, err := os.CreateTemp(dir, "hugo-*.partial")
	if err != nil {
		fail("cannot write to %s: %v", dir, err)
	}
	defer os.Remove(partial.Name()) // a no-op once the rename has happened
	if _, err := partial.Write(body); err != nil {
		fail("cannot write %s: %v", partial.Name(), err)
	}
	if err := partial.Chmod(0o755); err != nil {
		fail("cannot make %s executable: %v", partial.Name(), err)
	}
	if err := partial.Close(); err != nil {
		fail("cannot write %s: %v", partial.Name(), err)
	}
	if err := os.Rename(partial.Name(), binary); err != nil {
		fail("cannot move Hugo into %s: %v", binary, err)
	}
}

// hugoFromArchive returns the hugo executable out of a release tarball.
func hugoFromArchive(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no hugo executable in the archive")
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == "hugo" {
			return io.ReadAll(tr)
		}
	}
}

// requireHugoVersion fails unless the binary reports the pinned version. Hugo
// prints "hugo v0.166.0-<commit>+<edition> <os>/<arch> ...".
func requireHugoVersion(binary string) {
	out, err := exec.Command(binary, "version").Output()
	if err != nil {
		fail("cannot run %s version: %v", binary, err)
	}
	if !strings.HasPrefix(string(out), "hugo v"+hugoVersion+"-") {
		fail("%s is %s, but this exposer renders with Hugo %s (B-8)",
			binary, strings.TrimSpace(string(out)), hugoVersion)
	}
}
