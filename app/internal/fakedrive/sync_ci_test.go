// CI-only proof that the Linux Google Drive path works end-to-end.
//
// Real OAuth and Google's API are replaced by an rclone "alias" remote that
// points at a temp directory. The production code paths under test are:
// ResolveSyncSubfolder, EstablishBaseline, SyncOnce, and orchestrate.Setup.
// Players never see this stand-in.

package fakedrive_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"shogun2sync/internal/bisync"
	"shogun2sync/internal/config"
	"shogun2sync/internal/orchestrate"
	"shogun2sync/internal/rcloneutil"
)

const (
	ciRemoteName = "shogun2sync-ci-fake-gdrive"
	syncFolder   = "Shogun2SaveSync"
)

func requireLinuxRclone(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("fake Google Drive CI coverage exercises the Linux bisync path only")
	}
	if !rcloneutil.Installed() {
		t.Skip("rclone is not installed; CI installs the pinned release binary before go test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := bisync.CheckVersion(ctx); err != nil {
		t.Skip(err.Error())
	}
}

// isolateRcloneConfig points rclone at a throwaway config file so CI never
// reads or writes the developer's real ~/.config/rclone/rclone.conf.
func isolateRcloneConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "rclone.conf")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RCLONE_CONFIG", cfg)
}

// createAliasRemote makes remoteName resolve to rootDir, the way a Drive
// remote resolves to My Drive (host) or a shared folder (guest).
func createAliasRemote(t *testing.T, remoteName, rootDir string) {
	t.Helper()
	rclonePath, err := rcloneutil.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Delete any leftover definition from a previous attempt in this config.
	_ = exec.Command(rclonePath, "config", "delete", remoteName).Run()
	cmd := exec.Command(rclonePath, "config", "create", remoteName, "alias",
		"remote="+rootDir, "--non-interactive")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("config create alias remote: %v: %s", err, out)
	}
}

func TestCI_FakeGoogleDriveHostAndGuestShareSaves(t *testing.T) {
	requireLinuxRclone(t)
	isolateRcloneConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Layout mirrors production:
	//   myDrive/              ← host's Drive root (alias remote)
	//     Shogun2SaveSync/    ← shared folder both players mirror
	//   hostCloud/            ← host local GoogleDriveSync
	//   guestCloud/           ← guest local GoogleDriveSync
	//   hostSave/             ← game save_games_multiplayer (linked)
	//   guestSave/
	root := t.TempDir()
	myDrive := filepath.Join(root, "myDrive")
	hostCloud := filepath.Join(root, "hostCloud")
	guestCloud := filepath.Join(root, "guestCloud")
	hostSave := filepath.Join(root, "host", "save_games_multiplayer")
	guestSave := filepath.Join(root, "guest", "save_games_multiplayer")
	for _, dir := range []string{myDrive, hostCloud, guestCloud, hostSave, guestSave} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// --- Host: full Drive root, create subfolder, baseline, link game ---
	createAliasRemote(t, ciRemoteName, myDrive)

	hostSub, err := rcloneutil.ResolveSyncSubfolder(ctx, ciRemoteName, syncFolder, false)
	if err != nil {
		t.Fatalf("host ResolveSyncSubfolder: %v", err)
	}
	if hostSub != syncFolder {
		t.Fatalf("host remote subfolder = %q, want %q", hostSub, syncFolder)
	}
	// Creating the subfolder a second time must not nest.
	again, err := rcloneutil.ResolveSyncSubfolder(ctx, ciRemoteName, syncFolder, false)
	if err != nil {
		t.Fatalf("host re-resolve: %v", err)
	}
	if again != syncFolder {
		t.Fatalf("second host resolve nested: %q", again)
	}

	hostLocal := filepath.Join(hostCloud, syncFolder)
	if err := bisync.EstablishBaseline(ctx, ciRemoteName, hostSub, hostLocal, false); err != nil {
		t.Fatalf("host EstablishBaseline: %v", err)
	}
	// Prove the remote side got the folder without nesting.
	entries, err := os.ReadDir(myDrive)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != syncFolder || !entries[0].IsDir() {
		t.Fatalf("myDrive layout wrong after host baseline: %v", names(entries))
	}
	if _, err := os.Stat(filepath.Join(myDrive, syncFolder, syncFolder)); !os.IsNotExist(err) {
		t.Fatal("host baseline created nested Shogun2SaveSync/Shogun2SaveSync")
	}

	hostCfg := config.Config{
		CloudProvider: "googledrive",
		CloudRoot:     hostCloud,
		SyncSubfolder: syncFolder,
	}
	if res := orchestrate.Setup(hostCfg, hostSave); !res.OK {
		t.Fatalf("host Setup: %s", res.Error)
	}

	// Host "plays" a turn: write a multiplayer save through the game path.
	saveName := "multiplayer_campaign_ci_turn1.save_multiplayer"
	hostSaveFile := filepath.Join(hostSave, saveName)
	if err := os.WriteFile(hostSaveFile, []byte("host-turn-1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Because hostSave is linked to hostLocal, the file is already in the
	// mirror directory; push it to the fake Drive.
	if err := bisync.SyncOnce(ctx, ciRemoteName, hostSub, hostLocal); err != nil {
		t.Fatalf("host SyncOnce after write: %v", err)
	}
	driveCopy := filepath.Join(myDrive, syncFolder, saveName)
	if got, err := os.ReadFile(driveCopy); err != nil || string(got) != "host-turn-1" {
		t.Fatalf("fake Drive missing host save: contents=%q err=%v", got, err)
	}

	// --- Guest: remote rooted at the shared folder (as root_folder_id does) ---
	sharedFolder := filepath.Join(myDrive, syncFolder)
	createAliasRemote(t, ciRemoteName, sharedFolder)

	guestSub, err := rcloneutil.ResolveSyncSubfolder(ctx, ciRemoteName, syncFolder, true)
	if err != nil {
		t.Fatalf("guest ResolveSyncSubfolder: %v", err)
	}
	if guestSub != "" {
		t.Fatalf("guest must mirror remote root, got subfolder %q", guestSub)
	}
	guestLocal := filepath.Join(guestCloud, syncFolder)
	if err := bisync.EstablishBaseline(ctx, ciRemoteName, guestSub, guestLocal, true); err != nil {
		t.Fatalf("guest EstablishBaseline: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(guestLocal, saveName)); err != nil || string(got) != "host-turn-1" {
		t.Fatalf("guest local mirror missing host save after baseline: contents=%q err=%v", got, err)
	}

	guestCfg := config.Config{
		CloudProvider: "googledrive",
		CloudRoot:     guestCloud,
		SyncSubfolder: syncFolder,
	}
	if res := orchestrate.Setup(guestCfg, guestSave); !res.OK {
		t.Fatalf("guest Setup: %s", res.Error)
	}
	if got, err := os.ReadFile(filepath.Join(guestSave, saveName)); err != nil || string(got) != "host-turn-1" {
		t.Fatalf("guest game path missing host save after Setup: contents=%q err=%v", got, err)
	}

	// Guest plays the next turn; host must pick it up after both sync.
	guestTurn := "multiplayer_campaign_ci_turn2.save_multiplayer"
	if err := os.WriteFile(filepath.Join(guestSave, guestTurn), []byte("guest-turn-2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bisync.SyncOnce(ctx, ciRemoteName, guestSub, guestLocal); err != nil {
		t.Fatalf("guest SyncOnce: %v", err)
	}
	// Host still uses Drive-root remote + subfolder.
	createAliasRemote(t, ciRemoteName, myDrive)
	if err := bisync.SyncOnce(ctx, ciRemoteName, hostSub, hostLocal); err != nil {
		t.Fatalf("host SyncOnce to pull guest turn: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(hostSave, guestTurn)); err != nil || string(got) != "guest-turn-2" {
		t.Fatalf("host did not receive guest turn: contents=%q err=%v", got, err)
	}

	// Nested-folder regression: guest must never create shared/Shogun2SaveSync.
	if _, err := os.Stat(filepath.Join(sharedFolder, syncFolder)); !os.IsNotExist(err) {
		t.Fatal("guest path re-introduced nested Shogun2SaveSync under the shared folder")
	}
}

func TestCI_FakeGoogleDriveHostReusesExistingFolder(t *testing.T) {
	requireLinuxRclone(t)
	isolateRcloneConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	root := t.TempDir()
	myDrive := filepath.Join(root, "myDrive")
	// Pre-create the sync folder as if a previous host run already did.
	if err := os.MkdirAll(filepath.Join(myDrive, syncFolder), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(myDrive, syncFolder, "existing.save_multiplayer"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	createAliasRemote(t, ciRemoteName, myDrive)

	sub, err := rcloneutil.ResolveSyncSubfolder(ctx, ciRemoteName, syncFolder, false)
	if err != nil {
		t.Fatal(err)
	}
	if sub != syncFolder {
		t.Fatalf("expected reuse of existing folder, got %q", sub)
	}
	// Still only one level under myDrive.
	entries, err := os.ReadDir(myDrive)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != syncFolder {
		t.Fatalf("unexpected myDrive entries: %v", names(entries))
	}
}

func TestCI_FakeGoogleDriveGuestNeverCreatesSubfolder(t *testing.T) {
	requireLinuxRclone(t)
	isolateRcloneConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	shared := t.TempDir()
	if err := os.WriteFile(filepath.Join(shared, "from-host.save_multiplayer"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	createAliasRemote(t, ciRemoteName, shared)

	sub, err := rcloneutil.ResolveSyncSubfolder(ctx, ciRemoteName, syncFolder, true)
	if err != nil {
		t.Fatal(err)
	}
	if sub != "" {
		t.Fatalf("guest subfolder = %q, want empty", sub)
	}
	// Shared root must not gain a nested folder name.
	if _, err := os.Stat(filepath.Join(shared, syncFolder)); !os.IsNotExist(err) {
		t.Fatal("guest ResolveSyncSubfolder created a nested sync folder")
	}
}

func names(entries []os.DirEntry) string {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(e.Name())
	}
	return b.String()
}
