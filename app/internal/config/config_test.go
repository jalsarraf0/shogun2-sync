package config

import "testing"

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
