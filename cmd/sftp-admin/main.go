// Command sftp-admin serves the SFTP user admin UI.
//
// Required environment:
//
//	SFTP_ADMIN_PASSWORD  admin password; missing value fails fast.
//	PORT                 listen port or address (default :8080).
//	SFTP_USERS_FILE      manifest path (default /var/lib/sftp-users/users.json).
//	SFTP_SEED_USER / SFTP_SEED_PUBKEY
//	                     optional first user, honored only when the manifest
//	                     file does not exist yet.
package main

import (
	"log"
	"net/http"
	"os"

	"simple-fileurl/internal/sftpadmin"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := sftpadmin.LoadConfig(nil)
	if err != nil {
		return err
	}
	st := sftpadmin.NewStore(cfg.UsersFile)
	if err := seedIfAbsent(st, cfg); err != nil {
		return err
	}
	if err := sftpadmin.SeedSharedConfig(cfg.SharedPath, nil); err != nil {
		return err
	}
	srv := sftpadmin.NewServer(cfg, st)
	log.Printf("sftp-admin listening on %s manifest=%s", cfg.Addr, cfg.UsersFile)
	return http.ListenAndServe(cfg.Addr, srv)
}

// seedIfAbsent creates the optional first user only when no manifest file
// exists yet; an existing manifest is never overwritten or merged.
func seedIfAbsent(st *sftpadmin.Store, cfg sftpadmin.Config) error {
	if cfg.SeedUser == "" {
		return nil
	}
	if _, err := os.Stat(cfg.UsersFile); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	pub, _, err := sftpadmin.ValidatePublicKey(cfg.SeedPubKey)
	if err != nil {
		return err
	}
	return st.Update(func(m *sftpadmin.Manifest) error {
		m.Users = append(m.Users, sftpadmin.User{
			Username:       cfg.SeedUser,
			Enabled:        true,
			AuthorizedKeys: []string{pub},
		})
		return nil
	})
}
