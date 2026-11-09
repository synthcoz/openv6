package panel

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
)

// staticFile is one embedded web asset, prepared once: content type, ETag and
// a gzip copy, so the dashboard loads fast and is revalidated, not re-downloaded.
type staticFile struct {
	body, gz []byte
	ctype    string
	etag     string
}

type staticHandler struct {
	fsys  fs.FS
	once  sync.Once
	files map[string]*staticFile
}

func (h *staticHandler) load() {
	h.files = map[string]*staticFile{}
	_ = fs.WalkDir(h.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(h.fsys, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		f := &staticFile{body: b, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		// fixed types: on Windows the registry can map .js to text/plain,
		// which browsers refuse to run with nosniff
		f.ctype = map[string]string{".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
			".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".json": "application/json"}[path.Ext(p)]
		if f.ctype == "" {
			f.ctype = mime.TypeByExtension(path.Ext(p))
		}
		if f.ctype == "" {
			f.ctype = http.DetectContentType(b)
		}
		if len(b) > 1024 {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(b)
			_ = zw.Close()
			if buf.Len() < len(b) {
				f.gz = buf.Bytes()
			}
		}
		h.files[p] = f
		return nil
	})
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.once.Do(h.load)
	p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	f := h.files[p]
	if f == nil {
		http.NotFound(w, r)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", f.ctype)
	hd.Set("ETag", f.etag)
	hd.Set("Cache-Control", "no-cache") // always revalidate: updates show up at once
	hd.Add("Vary", "Accept-Encoding")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := f.body
	if f.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		hd.Set("Content-Encoding", "gzip")
		body = f.gz
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}
