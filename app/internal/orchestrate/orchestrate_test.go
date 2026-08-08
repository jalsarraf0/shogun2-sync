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

// A player who typed in a non-default save path can't be helped by
// auto-detection afterwards either — Status has to trust the recorded path,
// or it reports a working setup as broken on every later launch.
func TestStatusUsesRecordedSavePath(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	res := Setup(cfg, savePath)
	if !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	cfg.SavePath = res.SavePath

	st := Status(cfg)
	if st.SavePath != savePath {
		t.Errorf("Status().SavePath = %q, want %q", st.SavePath, savePath)
	}
	if !st.Linked || !st.LinkedOK {
		t.Errorf("expected Status to see a healthy link, got %+v", st)
	}
}

// The same recorded path also lets a re-run of Setup skip straight to
// "already set up" instead of failing to find the folder again.
func TestSetupFallsBackToRecordedSavePath(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	if res := Setup(cfg, savePath); !res.OK {
		t.Fatalf("first Setup failed: %s", res.Error)
	}
	cfg.SavePath = savePath

	res := Setup(cfg, "") // no override this time
	if !res.OK || !res.AlreadySet {
		t.Fatalf("expected AlreadySet using the recorded path, got %+v", res)
	}
}

// A recorded path that no longer exists must not be used: falling through
// to detection is right, and linking into a stale location would be wrong.
func TestSetupIgnoresStaleRecordedSavePath(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{
		CloudProvider: "dropbox",
		CloudRoot:     cloudRoot,
		SyncSubfolder: "Shogun2SaveSync",
		SavePath:      filepath.Join(t.TempDir(), "gone"),
	}

	res := Setup(cfg, savePath)
	if !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	if res.SavePath != savePath {
		t.Errorf("SavePath = %q, want the override %q", res.SavePath, savePath)
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
