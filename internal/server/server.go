package server

import (
	"errors"
	"net/http"
	"os"
	"path"
	"strings"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/links"
	"simple-fileurl/internal/store"
)

// Server serves the file-sharing HTTP endpoints.
type Server struct {
	cfg   config.Config
	store *store.Store
	links *links.Store
	mux   *http.ServeMux
}

// New creates a Server. Callers must ensure cfg is valid.
func New(cfg config.Config, st *store.Store, ls *links.Store) *Server {
	s := &Server{cfg: cfg, store: st, links: ls, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /", s.handleEmpty)
	s.mux.HandleFunc("POST /", s.handleEmpty)
	s.mux.HandleFunc("GET /{dirHash}/{fileHash}", s.handleDownload)
	s.mux.HandleFunc("POST /api/links", s.handleLinksCreate)
	s.mux.HandleFunc("GET /api/links", s.handleLinksList)
	s.mux.HandleFunc("GET /api/links/{id}", s.handleLinkGet)
	s.mux.HandleFunc("PATCH /api/links/{id}", s.handleLinkPatch)
	s.mux.HandleFunc("DELETE /api/links/{id}", s.handleLinkDelete)
	s.mux.HandleFunc("GET /l/{id}", s.handleLinkPage)
	s.mux.HandleFunc("POST /l/{id}/auth", s.handleLinkAuth)
	s.mux.HandleFunc("GET /l/{id}/files", s.handleLinkFiles)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// effectiveConfig overlays the shared config file on the startup config.
// A missing or invalid file leaves the startup config unchanged.
func (s *Server) effectiveConfig() config.Config {
	cfg := s.cfg
	path := cfg.SharedPath
	if path == "" {
		path = config.DefaultSharedConfigPath
	}
	sc, err := config.LoadShared(path)
	if err != nil {
		return cfg
	}
	cfg.HashAlgorithm = sc.HashAlgorithm
	cfg.HashTarget = sc.HashTarget
	cfg.PublicURL = sc.PublicURL
	cfg.AdminToken = sc.AdminToken
	return cfg
}

// reloadLinks refreshes the links store from disk so links created or
// changed by the sftp-admin container become visible without a restart. A
// missing or unreadable file leaves the in-memory store unchanged,
// mirroring effectiveConfig's fallback.
func (s *Server) reloadLinks() {
	if err := s.links.Load(); err != nil {
		return
	}
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
	abs, entry, err := s.store.ResolveWith(s.effectiveConfig(), dirHash, fileHash)
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

// handleEmpty is the catch-all for index paths: 200 with no content,
// revealing nothing about the service or the shared files. File listings
// live behind scoped share links at /l/:linkId.
func (s *Server) handleEmpty(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}
