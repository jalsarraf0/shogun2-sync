package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func isolateConfigHome(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", root)
	case "darwin":
		t.Setenv("HOME", root)
	default:
		t.Setenv("XDG_CONFIG_HOME", root)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolateConfigHome(t)

	if Exists() {
		t.Fatal("Exists() should be false before any Save")
	}

	want := Config{
		CloudProvider: "dropbox",
		CloudRoot:     "/home/x/Dropbox",
		SyncSubfolder: "Shogun2SaveSync",
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists() {
		t.Fatal("Exists() should be true after Save")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestSaveUsesOwnerOnlyPermissionsEvenWhenReplacingOldConfig(t *testing.T) {
	isolateConfigHome(t)

	if err := Save(Config{CloudProvider: "dropbox"}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	p, err := path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{CloudProvider: "onedrive"}); err != nil {
		t.Fatalf("replacement Save: %v", err)
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %#o, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("config directory mode = %#o, want 0700", dirInfo.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(p), ".config-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary config files left behind: %v", matches)
	}
}

func TestGoogleCredentialsRequireIDAndSecretTogether(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "ID only", cfg: Config{GDriveClientID: "id"}},
		{name: "secret only", cfg: Config{GDriveClientSecret: "secret"}},
		{name: "whitespace ID only", cfg: Config{GDriveClientID: "  "}},
		{name: "whitespace secret only", cfg: Config{GDriveClientSecret: "\t"}},
		{name: "both whitespace", cfg: Config{GDriveClientID: "  ", GDriveClientSecret: "\t"}},
		{name: "blank ID", cfg: Config{GDriveClientID: "  ", GDriveClientSecret: "secret"}},
		{name: "blank secret", cfg: Config{GDriveClientID: "id", GDriveClientSecret: "\t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := tt.cfg.GoogleCredentials(); !errors.Is(err, ErrIncompleteGoogleCredentials) {
				t.Fatalf("GoogleCredentials error = %v, want ErrIncompleteGoogleCredentials", err)
			}
		})
	}
}

func TestGoogleCredentialsEmptyOrComplete(t *testing.T) {
	if id, secret, custom, err := (Config{}).GoogleCredentials(); err != nil || custom || id != "" || secret != "" {
		t.Fatalf("empty GoogleCredentials = (%q, %q, %v, %v)", id, secret, custom, err)
	}

	cfg := Config{GDriveClientID: "custom-id", GDriveClientSecret: "custom-secret"}
	id, secret, custom, err := cfg.GoogleCredentials()
	if err != nil || !custom || id != cfg.GDriveClientID || secret != cfg.GDriveClientSecret {
		t.Fatalf("custom GoogleCredentials = (%q, %q, %v, %v)", id, secret, custom, err)
	}
}

func TestSaveRejectsIncompleteCredentialsWithoutReplacingConfig(t *testing.T) {
	isolateConfigHome(t)
	want := Config{CloudProvider: "dropbox"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}

	err := Save(Config{CloudProvider: "onedrive", GDriveClientID: "id-only"})
	if !errors.Is(err, ErrIncompleteGoogleCredentials) {
		t.Fatalf("Save error = %v, want ErrIncompleteGoogleCredentials", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load after rejected Save = %+v, want %+v", got, want)
	}
}

func TestLoadRejectsIncompleteCredentials(t *testing.T) {
	isolateConfigHome(t)
	p, err := path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"gdrive_client_secret":"secret-only"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); !errors.Is(err, ErrIncompleteGoogleCredentials) {
		t.Fatalf("Load error = %v, want ErrIncompleteGoogleCredentials", err)
	}
}
