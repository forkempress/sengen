package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"sync"
)

// Cloudflare caches .css and .js at the edge for hours, so a deploy that
// changes style.css kept serving the old file under the same URL. Templates
// link static files through assetURL, which appends a hash of the content:
// a changed file gets a new URL and the old cached copy is never asked for.

var (
	assetHashOnce sync.Once
	assetHashes   map[string]string
)

func staticFiles() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embedded at build time; a failure here means the binary is broken
	}
	return sub
}

// assetURL returns /static/<name>?v=<first 10 hex chars of sha256>.
func assetURL(name string) string {
	assetHashOnce.Do(func() {
		assetHashes = map[string]string{}
		_ = fs.WalkDir(staticFiles(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(staticFiles(), p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			assetHashes[p] = hex.EncodeToString(sum[:])[:10]
			return nil
		})
	})
	if h, ok := assetHashes[name]; ok {
		return "/static/" + name + "?v=" + h
	}
	return "/static/" + name
}

// staticHandler serves embedded files. A versioned URL never changes content,
// so it may be cached for a year; an unversioned one must be revalidated.
func staticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(staticFiles()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
