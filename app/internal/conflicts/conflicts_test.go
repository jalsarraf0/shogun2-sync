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

func TestScanFindsDropboxAndOneDriveConflictNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "turn090.save")
	write(t, dir, "turn090 (Friend's conflicted copy 2026-08-07).save")
	write(t, dir, "turn091-conflict-PCNAME.save")

	got, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Scan found %d conflicts, want 2: %+v", len(got), got)
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
