package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// B-7: serve the assembled artifact for preview. It exists so a preview is
// looked at the same way the deployed site will be, rather than through
// whatever static server happens to be installed.
//
// The point is fidelity to an ordinary static host, not features: a directory resolves
// to its index.html, a path without its trailing slash redirects to the one
// with it, and anything missing gets the artifact's own 404 page if it has one.
// No caching headers, since a preview that caches is a preview that lies.
func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	target := fs.String("target", "target", "build directory whose site/ to serve")
	site := fs.String("site", "", "assembled artifact to serve (default: <target>/site)")
	addr := fs.String("addr", "127.0.0.1:8888", "address to listen on")
	fs.Parse(args)
	if *site == "" {
		*site = filepath.Join(*target, "site")
	}

	info, err := os.Stat(*site)
	if err != nil || !info.IsDir() {
		fail("%s is not a directory — run exposer build first", *site)
	}

	// The host's MIME table is not part of this repo, and a preview that serves
	// AVIF as application/octet-stream silently falls back to JPEG, which is
	// exactly the comparison a preview is for.
	for ext, kind := range map[string]string{
		".avif":        "image/avif",
		".woff2":       "font/woff2",
		".webmanifest": "application/manifest+json",
	} {
		if err := mime.AddExtensionType(ext, kind); err != nil {
			fail("cannot register %s: %v", ext, err)
		}
	}

	root := http.Dir(*site) // rejects .. itself, so a request cannot escape the artifact
	files := http.FileServer(root)
	notFound := filepath.Join(*site, "404.html")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if missing(root, r.URL.Path) {
			serveNotFound(w, r, notFound)
			return
		}
		files.ServeHTTP(w, r)
	})

	// Bind before announcing: a port already in use is the common failure here
	// (something else on the port), and printing the address first would claim a
	// preview that never started.
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fail("cannot listen on %s: %v", *addr, err)
	}
	server := &http.Server{
		Handler:           logged(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("serving %s at http://%s/ — ctrl-c to stop\n", *site, listener.Addr())
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fail("cannot serve on %s: %v", *addr, err)
	}
}

// missing reports whether a path has nothing behind it, so the artifact's own
// 404 page can answer instead of net/http's bare "404 page not found". A
// directory counts as present only when it holds an index.html, which is what
// Apache serves and what every generated URL ends in.
func missing(root http.Dir, upath string) bool {
	if !strings.HasPrefix(upath, "/") {
		return true
	}
	f, err := root.Open(upath)
	if err != nil {
		return true
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return true
	}
	if !info.IsDir() {
		return false
	}
	// A directory without a trailing slash still redirects rather than 404s, so
	// leave that to the file server and only judge the one it will land on.
	index, err := root.Open(strings.TrimSuffix(upath, "/") + "/index.html")
	if err != nil {
		return true
	}
	index.Close()
	return false
}

func serveNotFound(w http.ResponseWriter, r *http.Request, page string) {
	body, err := os.ReadFile(page)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write(body)
}

// logged prints one line per request, which is how a preview shows that a page
// is asking for something the artifact does not contain.
func logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%3d %s", rec.status, r.URL.Path)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
