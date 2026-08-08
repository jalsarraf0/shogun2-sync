// Package config stores this app's settings (which cloud provider, which
// folder) in the OS-standard per-user config location, so there's nothing
// for a non-technical user to hand-edit.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	CloudProvider string `json:"cloud_provider"` // "dropbox" | "onedrive" | "googledrive"
	CloudRoot     string `json:"cloud_root"`     // folder the cloud client syncs to disk
	SyncSubfolder string `json:"sync_subfolder"` // subfolder inside CloudRoot dedicated to this app
	SavePath      string `json:"save_path,omitempty"`

	// GDriveClientID/Secret let a player use their own Google OAuth client
	// instead of the one this app ships with. rclone's docs say the shared
	// client it lends us "is being retired and will stop working during
	// 2026", and when that happens no update to this app can fix it for
	// someone — only their own client ID can. Empty means "use the
	// built-in one", which is still the normal case.
	GDriveClientID     string `json:"gdrive_client_id,omitempty"`
	GDriveClientSecret string `json:"gdrive_client_secret,omitempty"`
}

// GoogleCredentials returns the player's own OAuth client if they set one.
func (c Config) GoogleCredentials() (id, secret string, custom bool) {
	if c.GDriveClientID != "" && c.GDriveClientSecret != "" {
		return c.GDriveClientID, c.GDriveClientSecret, true
	}
	return "", "", false
}

func dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "shogun2sync"), nil
}

func path() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Exists reports whether a config file has already been written.
func Exists() bool {
	p, err := path()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Load reads the saved config. Callers should check Exists first.
func Load() (Config, error) {
	var cfg Config
	p, err := path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

// Save writes the config, creating its directory if needed.
func Save(cfg Config) error {
	d, err := dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
