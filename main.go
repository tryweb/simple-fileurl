package main

import (
	"log"
	"net/http"
	"os"

	"simple-fileurl/internal/config"
	"simple-fileurl/internal/links"
	"simple-fileurl/internal/server"
	"simple-fileurl/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	st := store.New(cfg)
	// Fail fast when the namespace is missing instead of serving an
	// unintended directory.
	if err := st.Check(); err != nil {
		return err
	}
	ls := links.NewStore(cfg.LinksDir)
	if err := ls.Load(); err != nil {
		return err
	}
	srv := server.New(cfg, st, ls)
	log.Printf("listening on :%s prefix=%s target=%s algo=%s", cfg.Port, cfg.SharePrefix, cfg.HashTarget, cfg.HashAlgorithm)
	return http.ListenAndServe(":"+cfg.Port, srv)
}
