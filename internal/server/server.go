package server

import (
	"errors"
	"html/template"
	"net/http"
	"os"
	"path"
	"strings"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/store"
)

// Server serves the file-sharing HTTP endpoints.
type Server struct {
	cfg   config.Config
	store *store.Store
	mux   *http.ServeMux
}

// New creates a Server. Callers must ensure cfg is valid.
func New(cfg config.Config, st *store.Store) *Server {
	s := &Server{cfg: cfg, store: st, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /{dirHash}/{fileHash}", s.handleDownload)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.Check(); err != nil {
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	dirHash := r.PathValue("dirHash")
	fileHash := r.PathValue("fileHash")
	abs, entry, err := s.store.Resolve(dirHash, fileHash)
	switch {
	case errors.Is(err, store.ErrInvalid):
		http.Error(w, "invalid share link", http.StatusBadRequest)
		return
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "ambiguous share link", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	name := path.Base(entry.LogicalPath)
	w.Header().Set("Content-Disposition", "attachment; filename="+safeFilename(name))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// safeFilename neutralizes header-injection characters in an
// uploader-controlled basename: a quote or backslash could break out of the
// quoted-string, and control bytes are illegal in header values.
// Other runes (including CJK names) pass through untouched.
func safeFilename(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, name)
	if clean == "" || clean == "." || clean == ".." {
		clean = "download"
	}
	return `"` + clean + `"`
}

var indexTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>File sharing</title></head>
<body>
<h1>Shared files</h1>
<p>Hash target: {{.HashTarget}}; algorithm: {{.HashAlgorithm}}</p>
{{if .Entries}}
<ul>
{{range .Entries}}<li>{{.LogicalPath}}<br><a href="{{.URL}}">{{.URL}}</a></li>
{{end}}</ul>
{{else}}
<p>No files available.</p>
{{end}}
</body>
</html>`))

type indexEntry struct {
	LogicalPath string
	URL         string
}

type indexData struct {
	HashTarget    string
	HashAlgorithm string
	Entries       []indexEntry
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	entries, err := s.store.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := indexData{HashTarget: s.cfg.HashTarget, HashAlgorithm: s.cfg.HashAlgorithm}
	for _, e := range entries {
		data.Entries = append(data.Entries, indexEntry{
			LogicalPath: e.LogicalPath,
			URL:         s.cfg.ShareURL(e.DirHash, e.FileHash),
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
