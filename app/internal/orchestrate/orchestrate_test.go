package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shogun2sync/internal/config"
	"shogun2sync/internal/linkutil"
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
	linkStatus, err := linkutil.Inspect(savePath, SyncTarget(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if !linkStatus.IsLink || !linkStatus.MatchesTarget {
		t.Fatalf("expected savePath to link to the sync target, got %+v", linkStatus)
	}
}

// CloudRoot that is already the sync folder must not gain another subfolder
// level — that is the nested-folder bug on every re-setup.
func TestSyncTargetDoesNotDoubleNestWhenRootIsAlreadyTheFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Shogun2SaveSync")
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: root, SyncSubfolder: "Shogun2SaveSync"}
	if got := SyncTarget(cfg); got != root {
		t.Fatalf("SyncTarget = %q, want %q", got, root)
	}
}

func TestFlattenSameNameNestingHoistsDeepSaves(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "Shogun2SaveSync", "Shogun2SaveSync", "Shogun2SaveSync")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "turn50.save_multiplayer"), []byte("deep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "already.save"), []byte("top"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Same basename at root and deep must keep both copies.
	if err := os.WriteFile(filepath.Join(deep, "already.save"), []byte("deep-copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := FlattenSameNameNesting(root, "Shogun2SaveSync"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "turn50.save_multiplayer")); err != nil {
		t.Fatalf("deep save was not hoisted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Shogun2SaveSync")); !os.IsNotExist(err) {
		t.Fatalf("nested folder should be gone, err=%v", err)
	}
	// Original top-level file still present.
	if got, err := os.ReadFile(filepath.Join(root, "already.save")); err != nil || string(got) != "top" {
		t.Fatalf("top-level save changed: %q err=%v", got, err)
	}
}

func TestSetupFlattensExistingNestedSyncFolder(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}
	// Simulate the nested mess before setup links the game.
	deep := filepath.Join(SyncTarget(cfg), "Shogun2SaveSync", "Shogun2SaveSync")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "nested.save_multiplayer"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := Setup(cfg, savePath)
	if !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	if _, err := os.Stat(filepath.Join(SyncTarget(cfg), "nested.save_multiplayer")); err != nil {
		t.Fatalf("setup should flatten nested saves into the target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(SyncTarget(cfg), "Shogun2SaveSync")); !os.IsNotExist(err) {
		t.Fatal("setup left a nested Shogun2SaveSync directory")
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

func TestUndoRestoresARealFolderAndKeepsTheCloudCopy(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	res := Setup(cfg, savePath)
	if !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	cfg.SavePath = res.SavePath

	undo := Undo(cfg)
	if !undo.OK {
		t.Fatalf("Undo failed: %s", undo.Error)
	}
	if undo.Restored != 1 {
		t.Errorf("Restored = %d, want 1", undo.Restored)
	}

	// A real directory again, not a link.
	info, err := os.Lstat(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("expected the save path to be a real folder after Undo, got %s", info.Mode())
	}
	linkStatus, err := linkutil.Inspect(savePath, SyncTarget(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if linkStatus.IsLink {
		t.Fatal("expected the save path not to be a link after Undo")
	}
	if _, err := os.Stat(filepath.Join(savePath, "turn090.save")); err != nil {
		t.Fatalf("expected the save restored locally: %v", err)
	}

	// The other player is still syncing against the shared folder, so it
	// must survive one player backing out.
	if _, err := os.Stat(filepath.Join(SyncTarget(cfg), "turn090.save")); err != nil {
		t.Fatalf("Undo removed the shared copy: %v", err)
	}

	if st := Status(cfg); st.LinkedOK {
		t.Error("Status should no longer report the folder as linked")
	}
}

func TestUndoOnAnUnlinkedFolderChangesNothing(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{
		CloudProvider: "dropbox", CloudRoot: cloudRoot,
		SyncSubfolder: "Shogun2SaveSync", SavePath: savePath,
	}

	undo := Undo(cfg)
	if undo.OK {
		t.Fatal("expected Undo to refuse when nothing is linked")
	}
	if _, err := os.Stat(filepath.Join(savePath, "turn090.save")); err != nil {
		t.Fatalf("Undo touched an unlinked save folder: %v", err)
	}
}

// Setup must be reachable again after an Undo, or a player who backs out
// once is stuck for good.
func TestSetupWorksAgainAfterUndo(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}

	if res := Setup(cfg, savePath); !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	cfg.SavePath = savePath
	if undo := Undo(cfg); !undo.OK {
		t.Fatalf("Undo failed: %s", undo.Error)
	}
	res := Setup(cfg, savePath)
	if !res.OK {
		t.Fatalf("Setup after Undo failed: %s", res.Error)
	}
	if st := Status(cfg); !st.LinkedOK {
		t.Errorf("expected a healthy link after re-running Setup, got %+v", st)
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

	if err := ResolveConflict(cfg, rec.Conflicts[0].Path); err != nil {
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

func TestSetupRejectsDangerousSubfolderNames(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	for _, name := range []string{"", ".", "..", "../outside", "nested/folder", `nested\folder`, "CON", "bad:name"} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: name}
			res := Setup(cfg, savePath)
			if res.OK {
				t.Fatalf("Setup accepted dangerous sync folder %q", name)
			}
			if got, err := os.ReadFile(filepath.Join(savePath, "turn090.save")); err != nil || string(got) != "x" {
				t.Fatalf("failed validation changed the save: contents=%q err=%v", got, err)
			}
		})
	}
}

func TestSetupRejectsOverlappingSaveAndSyncPaths(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name, save, root, sub string
	}{
		{"same", filepath.Join(dir, "save_games_multiplayer"), dir, "save_games_multiplayer"},
		{"target inside saves", filepath.Join(dir, "nested", "save_games_multiplayer"), filepath.Join(dir, "nested", "save_games_multiplayer"), "cloud"},
		{"saves inside target", filepath.Join(dir, "cloud", "sync", "save_games_multiplayer"), filepath.Join(dir, "cloud"), "sync"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(tc.save, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(tc.root, 0o755); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(tc.save, "keep.save")
			if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{CloudProvider: "dropbox", CloudRoot: tc.root, SyncSubfolder: tc.sub}
			if res := Setup(cfg, tc.save); res.OK {
				t.Fatalf("Setup accepted overlapping paths: %+v", res)
			} else if !strings.Contains(res.Error, "overlapping") {
				t.Fatalf("Setup failed for the wrong reason: %s", res.Error)
			}
			if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
				t.Fatalf("overlap rejection changed save: contents=%q err=%v", got, err)
			}
		})
	}
}

func TestSetupRejectsWrongManualFolder(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "Shogun2")
	cloudRoot := filepath.Join(dir, "cloud")
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cloudRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(savePath, "do-not-move.txt")
	if err := os.WriteFile(marker, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}
	if res := Setup(cfg, savePath); res.OK {
		t.Fatalf("Setup accepted a parent directory: %+v", res)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "safe" {
		t.Fatalf("rejected directory was modified: contents=%q err=%v", got, err)
	}
}

func TestResolveConflictRejectsArbitraryFiles(t *testing.T) {
	_, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}
	if err := os.MkdirAll(SyncTarget(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.save")
	if err := os.WriteFile(outside, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ResolveConflict(cfg, outside); err == nil {
		t.Fatal("ResolveConflict accepted a file outside the sync folder")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "private" {
		t.Fatalf("outside file was changed: contents=%q err=%v", got, err)
	}

	ordinary := filepath.Join(SyncTarget(cfg), "ordinary.save")
	if err := os.WriteFile(ordinary, []byte("ordinary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ResolveConflict(cfg, ordinary); err == nil {
		t.Fatal("ResolveConflict accepted a non-conflict save")
	}
	if _, err := os.Stat(ordinary); err != nil {
		t.Fatalf("ordinary save was changed: %v", err)
	}
}

func TestPromoteConflictKeepsSelectedCopyAndPreservesOriginal(t *testing.T) {
	savePath, cloudRoot := setupFakeSave(t)
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}
	if res := Setup(cfg, savePath); !res.OK {
		t.Fatalf("Setup failed: %s", res.Error)
	}
	target := SyncTarget(cfg)
	original := filepath.Join(target, "turn090.save")
	conflict := filepath.Join(target, "turn090 (Friend's conflicted copy 2026-08-07).save")
	if err := os.WriteFile(conflict, []byte("new chosen state"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := PromoteConflict(cfg, conflict); err != nil {
		t.Fatalf("PromoteConflict failed: %v", err)
	}
	got, err := os.ReadFile(original)
	if err != nil || string(got) != "new chosen state" {
		t.Fatalf("promoted original contents=%q err=%v", got, err)
	}
	if _, err := os.Stat(conflict); !os.IsNotExist(err) {
		t.Fatalf("conflict name should be gone, stat err=%v", err)
	}
	trashEntries, err := os.ReadDir(filepath.Join(target, ".shogun2sync-trash"))
	if err != nil || len(trashEntries) != 1 {
		t.Fatalf("recovery trash entries=%v err=%v", trashEntries, err)
	}
	old, err := os.ReadFile(filepath.Join(target, ".shogun2sync-trash", trashEntries[0].Name()))
	if err != nil || string(old) != "x" {
		t.Fatalf("previous original was not preserved: contents=%q err=%v", old, err)
	}
}
