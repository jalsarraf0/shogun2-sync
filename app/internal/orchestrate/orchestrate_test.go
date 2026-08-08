package orchestrate

import (
	"os"
	"path/filepath"
	"testing"

	"shogun2sync/internal/config"
)

func setupFakeSave(t *testing.T) (savePath, cloudRoot string) {
	t.Helper()
	dir := t.TempDir()
	savePath = filepath.Join(dir, "save_games_multiplayer")
	cloudRoot = filepath.Join(dir, "fakecloud")
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cloudRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(savePath, "turn090.save"), []byte("x"), 0o644)
	return savePath, cloudRoot
}

func TestSetupMovesFilesAndLinks(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	res := Setup(cfg, savePath)
	if !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	if res.AlreadySet {
		t.Fatal("first Setup should not report AlreadySet")
	}

	if _, err := os.Stat(filepath.Join(SyncTarget(cfg), "turn090.save")); err != nil {
		t.Fatalf("expected save file moved into sync target: %v", err)
	}
	info, err := os.Lstat(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected savePath to become a symlink")
	}
}

func TestSetupIsIdempotent(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	if res := Setup(cfg, savePath); !res.OK {
		t.Fatalf("first Setup failed: %s", res.Error)
	}
	res := Setup(cfg, savePath)
	if !res.OK || !res.AlreadySet {
		t.Fatalf("second Setup should report AlreadySet, got %+v", res)
	}
}

func TestSetupRejectsMissingCloudRoot(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	os.RemoveAll(cloudRoot)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	res := Setup(cfg, savePath)
	if res.OK {
		t.Fatal("expected Setup to fail when cloud root doesn't exist")
	}
}

func TestRecoverFindsAndResolvesConflicts(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}
	if res := Setup(cfg, savePath); !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}

	target := SyncTarget(cfg)
	conflictPath := filepath.Join(target, "turn090 (Friend's conflicted copy 2026-08-07).save")
	os.WriteFile(conflictPath, []byte("x"), 0o644)

	rec := Recover(cfg)
	if !rec.OK || len(rec.Conflicts) != 1 {
		t.Fatalf("expected exactly 1 conflict, got %+v", rec)
	}

	if err := ResolveConflict(rec.Conflicts[0].Path); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}

	if _, err := os.Stat(conflictPath); !os.IsNotExist(err) {
		t.Fatal("expected conflict file to be moved out of the sync folder")
	}

	rec2 := Recover(cfg)
	if len(rec2.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts after resolve, got %+v", rec2)
	}
}
