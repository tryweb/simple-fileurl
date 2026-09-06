package sftpadmin

import (
	"fmt"
	"strings"
	"sync"

	"simple-fileurl/internal/config"
)

// SettingDef describes one editable setting for the settings page.
type SettingDef struct {
	// Key is the form field name, using the familiar ENV_VAR style.
	Key string
	// Service groups settings in the UI: "file-sharing" or "sftp-admin".
	Service string
	// Description is the one-line help shown under the input.
	Description string
	// AllowedValues documents the accepted values.
	AllowedValues string
	// Options, when non-empty, renders a select dropdown.
	Options []string
	// IsSecret renders a password input and redacts the value in logs.
	IsSecret bool
}

// SettingsRegistry lists every editable setting in display order.
var SettingsRegistry = []SettingDef{
	{Key: "HASH_ALGORITHM", Service: "file-sharing", Description: "Hash algorithm for share URLs", AllowedValues: "md5, sha256", Options: []string{"md5", "sha256"}},
	{Key: "HASH_TARGET", Service: "file-sharing", Description: "What gets hashed", AllowedValues: "file (contents), filename (basename)", Options: []string{"file", "filename"}},
	{Key: "PUBLIC_URL", Service: "file-sharing", Description: "External base URL for share links", AllowedValues: "non-empty URL"},
	{Key: "ADMIN_TOKEN", Service: "file-sharing", Description: "Bearer token for the share-link management API", AllowedValues: "non-empty secret", IsSecret: true},
	{Key: "SFTP_ADMIN_PASSWORD", Service: "sftp-admin", Description: "Password for this admin UI", AllowedValues: "non-empty", IsSecret: true},
}

// SettingValue pairs a definition with its current value.
type SettingValue struct {
	SettingDef
	Value string
}

// valueOf reads one setting from a SharedConfig.
func valueOf(key string, c config.SharedConfig) (string, error) {
	switch key {
	case "HASH_ALGORITHM":
		return c.HashAlgorithm, nil
	case "HASH_TARGET":
		return c.HashTarget, nil
	case "PUBLIC_URL":
		return c.PublicURL, nil
	case "ADMIN_TOKEN":
		return c.AdminToken, nil
	case "SFTP_ADMIN_PASSWORD":
		return c.SftpAdminPassword, nil
	default:
		return "", fmt.Errorf("unknown setting %q", key)
	}
}

// GetSettings reads the shared config file and returns current values
// merged with the registry metadata.
func GetSettings(configPath string) ([]SettingValue, error) {
	c, err := config.LoadShared(configPath)
	if err != nil {
		return nil, err
	}
	out := make([]SettingValue, 0, len(SettingsRegistry))
	for _, def := range SettingsRegistry {
		v, err := valueOf(def.Key, c)
		if err != nil {
			return nil, err
		}
		out = append(out, SettingValue{SettingDef: def, Value: v})
	}
	return out, nil
}

// settingsMu serializes in-process settings mutations so concurrent saves
// cannot interleave their load-modify-save cycles and lose each other's
// fields. Reads stay lock-free: Save publishes atomically, so a concurrent
// LoadShared observes either the old or the new complete document.
var settingsMu sync.Mutex

// ApplySetting validates one value, updates the shared config, and saves
// it atomically. Unknown keys and invalid values are rejected with the
// previous file untouched.
func ApplySetting(configPath, key, value string) error {
	errs, err := ApplySettings(configPath, map[string]string{key: value})
	if err != nil {
		return err
	}
	if msg, bad := errs[key]; bad {
		return fmt.Errorf("invalid setting %s: %s", key, msg)
	}
	return nil
}

// ApplySettings validates every submitted value against the current file
// and persists them as one load-once/save-once transaction: any invalid or
// unknown key aborts the whole save with the previous file untouched, so
// concurrent or half-valid submissions never leave partial persistence.
// Secrets submitted empty keep their current value. The returned map holds
// per-field errors keyed by setting name; a separate error reports a load
// or save failure affecting the entire transaction.
func ApplySettings(configPath string, vals map[string]string) (map[string]string, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	c, err := config.LoadShared(configPath)
	if err != nil {
		return nil, err
	}
	next := c
	errs := map[string]string{}
	for key, value := range vals {
		if isSecretSetting(key) && strings.TrimSpace(value) == "" {
			continue
		}
		trial := next
		switch key {
		case "HASH_ALGORITHM":
			trial.HashAlgorithm = value
		case "HASH_TARGET":
			trial.HashTarget = value
		case "PUBLIC_URL":
			trial.PublicURL = value
		case "ADMIN_TOKEN":
			trial.AdminToken = value
		case "SFTP_ADMIN_PASSWORD":
			trial.SftpAdminPassword = value
		default:
			errs[key] = fmt.Sprintf("unknown setting %q", key)
			continue
		}
		if err := trial.Validate(); err != nil {
			errs[key] = err.Error()
			continue
		}
		next = trial
	}
	if len(errs) > 0 {
		return errs, nil
	}
	return nil, next.Save(configPath)
}

// isSecretSetting reports whether key is a redacted secret setting.
func isSecretSetting(key string) bool {
	for _, def := range SettingsRegistry {
		if def.Key == key {
			return def.IsSecret
		}
	}
	return false
}
