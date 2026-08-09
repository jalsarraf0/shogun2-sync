package conflicts

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Each cloud client names its duplicates differently, and only Dropbox
// uses the word "conflict" — so this is really three separate features
// wearing one name.
func TestScanFindsEachProvidersDuplicateNaming(t *testing.T) {
	cases := []struct {
		provider string
		original string
		copy     string
	}{
		{"Dropbox", "Otomo Spring 1545.save", "Otomo Spring 1545 (Ken's conflicted copy 2026-08-07).save"},
		{"Dropbox case conflict", "Chosokabe 1546.save", "Chosokabe 1546 (Case Conflict).save"},
		{"OneDrive", "Otomo Spring 1545.save", "Otomo Spring 1545-DESKTOP-4KJ9P2A.save"},
		{"Google Drive", "Otomo Spring 1545.save", "Otomo Spring 1545 (1).save"},
		{"rclone bisync", "Otomo Spring 1545.save", "Otomo Spring 1545.save.conflict1"},
		{"rclone pre-1.66", "Otomo Spring 1545.save", "Otomo Spring 1545.save..path2"},
		{"MP autosave", "autosave.save_multiplayer", "autosave-LAPTOP-9911.save_multiplayer"},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.original)
			write(t, dir, tc.copy)

			got, err := Scan(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("Scan found %d conflicts, want exactly 1: %+v", len(got), got)
			}
			if got[0].Name != tc.copy {
				t.Errorf("flagged %q, want %q (the original must never be the one flagged)", got[0].Name, tc.copy)
			}
			if got[0].Reason == "" {
				t.Error("no reason given; the UI shows this so a player can overrule a bad guess")
			}
			if got[0].Original != tc.original {
				t.Errorf("Original = %q, want %q so either version can be kept", got[0].Original, tc.original)
			}
		})
	}
}

// The quieter patterns are shapes a player could plausibly type, so they
// only count when the file they'd be a copy of is sitting right there.
func TestScanLeavesOrdinarySaveNamesAlone(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Otomo Spring 1545.save",
		// Sits right next to "Otomo Spring 1545.save" and has OneDrive's
		// exact shape, but a lower-case suffix means a person typed it.
		"Otomo Spring 1545-final.save",
		"Otomo Spring 1545-backup.save",
		// No "Takeda.save" alongside, so the number is just part of a name.
		"Takeda (2).save",
		"autosave.save_multiplayer",
		"Shimazu Winter 1551.save",
	} {
		write(t, dir, name)
	}

	got, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Scan flagged ordinary saves: %+v", got)
	}
}

// Setup renames a colliding save rather than overwriting it; that rename
// has to come back out of Scan, or the player never learns there are two.
func TestScanFindsSetupRenames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Otomo Spring 1545.save")
	write(t, dir, "Otomo Spring 1545 (setup conflict 2026-08-08 174501).save")

	got, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Scan found %d, want 1: %+v", len(got), got)
	}
}

func TestScanIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "conflicted-copy-subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Scan should ignore directories, got %+v", got)
	}
}

func TestRecentOrdersNewestFirstAndCaps(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		write(t, dir, filepath.Base(dir)+string(rune('a'+i))+".save")
	}
	got, err := Recent(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("Recent(3) returned %d entries, want 3", len(got))
	}
}
