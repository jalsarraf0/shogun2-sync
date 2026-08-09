// Package config stores this app's settings (which cloud provider, which
// folder) in the OS-standard per-user config location, so there's nothing
// for a non-technical user to hand-edit.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	CloudProvider string `json:"cloud_provider"` // "dropbox" | "onedrive" | "googledrive"
	CloudRoot     string `json:"cloud_root"`     // folder the cloud client syncs to disk
	SyncSubfolder string `json:"sync_subfolder"` // subfolder inside CloudRoot dedicated to this app
	SavePath      string `json:"save_path,omitempty"`
	SetupComplete bool   `json:"setup_complete,omitempty"`

	// GDriveClientID/Secret let a player use their own Google OAuth client
	// instead of the one this app ships with. rclone's docs say the shared
	// client it lends us "is being retired and will stop working during
	// 2026", and when that happens no update to this app can fix it for
	// someone — only their own client ID can. Empty means "use the
	// built-in one", which is still the normal case.
	GDriveClientID     string `json:"gdrive_client_id,omitempty"`
	GDriveClientSecret string `json:"gdrive_client_secret,omitempty"`
}

// ErrIncompleteGoogleCredentials means only one half of a custom OAuth
// client was configured. Falling back to the shared client in that state is
// surprising and can make a typo look like a successful save.
var ErrIncompleteGoogleCredentials = errors.New("Google client ID and client secret must be provided together")

// Validate checks invariants which must also hold for configs loaded from an
// older version or edited by hand.
func (c Config) Validate() error {
	idSet := strings.TrimSpace(c.GDriveClientID) != ""
	secretSet := strings.TrimSpace(c.GDriveClientSecret) != ""
	blankID := c.GDriveClientID != "" && !idSet
	blankSecret := c.GDriveClientSecret != "" && !secretSet
	if idSet != secretSet || blankID || blankSecret {
		return ErrIncompleteGoogleCredentials
	}
	return nil
}

// GoogleCredentials returns the player's own OAuth client if they set one.
func (c Config) GoogleCredentials() (id, secret string, custom bool, err error) {
	if err := c.Validate(); err != nil {
		return "", "", false, err
	}
	if strings.TrimSpace(c.GDriveClientID) != "" {
		return c.GDriveClientID, c.GDriveClientSecret, true, nil
	}
	return "", "", false, nil
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
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

// Save atomically writes the config, creating its directory if needed. The
// file contains an OAuth client secret when custom credentials are used, so
// it is always replaced with owner-only permissions rather than inheriting
// the mode of an older installation.
func Save(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	d, err := dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	// MkdirAll preserves an existing directory's mode. Older releases used
	// looser permissions, so tighten it before writing a file that may contain
	// a custom OAuth client secret.
	if err := os.Chmod(d, 0o700); err != nil {
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
	return atomicWriteFile(p, data, 0o600)
}

func atomicWriteFile(name string, data []byte, mode os.FileMode) (retErr error) {
	d := filepath.Dir(name)
	tmp, err := os.CreateTemp(d, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err := os.Remove(tmpName); err != nil && !errors.Is(err, os.ErrNotExist) && retErr == nil {
			retErr = err
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, name); err != nil {
		return err
	}
	// The file contents and rename are durable on filesystems that support
	// syncing directory handles. Some platforms do not, so this is best-effort.
	if dir, err := os.Open(d); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
