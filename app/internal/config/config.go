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
	CloudRoot     string `json:"cloud_root"`      // folder the cloud client syncs to disk
	SyncSubfolder string `json:"sync_subfolder"`  // subfolder inside CloudRoot dedicated to this app
	SavePath      string `json:"save_path,omitempty"`
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
