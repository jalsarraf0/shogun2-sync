package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shogun2sync/internal/config"
)

func TestGoogleMirrorLocationKeepsHostAndGuestOnSameLocalFolder(t *testing.T) {
	hostRemote, hostLocal, hostRootScoped := googleMirrorLocation("", "Shogun2SaveSync")
	guestRemote, guestLocal, guestRootScoped := googleMirrorLocation("shared-folder-id", "Shogun2SaveSync")
	if hostRemote != "Shogun2SaveSync" || hostRootScoped {
		t.Fatalf("host mapping = remote %q rootScoped=%v", hostRemote, hostRootScoped)
	}
	if guestRemote != "" || !guestRootScoped {
		t.Fatalf("guest mapping = remote %q rootScoped=%v", guestRemote, guestRootScoped)
	}
	if hostLocal != guestLocal || filepath.Base(hostLocal) != "Shogun2SaveSync" {
		t.Fatalf("host local %q and guest local %q must be the exact same layout", hostLocal, guestLocal)
	}
}

func TestRunSetupPersistsOnlyCompletedSetup(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	root := t.TempDir()
	cloudRoot := filepath.Join(root, "cloud")
	savePath := filepath.Join(root, "save_games_multiplayer")
	if err := os.MkdirAll(cloudRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(savePath, "turn.save"), []byte("save"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.startup(context.Background())
	cfg := config.Config{CloudProvider: "dropbox", CloudRoot: cloudRoot, SyncSubfolder: "Shogun2SaveSync"}
	result := app.RunSetup(cfg, savePath)
	if !result.OK {
		t.Fatalf("RunSetup failed: %s", result.Error)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !saved.SetupComplete || saved.SavePath != savePath {
		t.Fatalf("saved config does not record completed setup: %+v", saved)
	}
	if !app.ConfigExists() {
		t.Fatal("completed setup should be recognized on restart")
	}
	if runtime.GOOS == "linux" {
		auth := app.AuthorizeGoogleDrive("", "DifferentSyncFolder", "", "")
		if auth.OK || !strings.Contains(auth.Error, "Stop syncing") {
			t.Fatalf("Google reconfiguration should require stopping the healthy existing link first: %+v", auth)
		}
	}

	undo := app.RunUndo()
	if !undo.OK {
		t.Fatalf("RunUndo failed: %s", undo.Error)
	}
	if app.ConfigExists() {
		t.Fatal("stopped setup should not boot as actively configured")
	}
}
