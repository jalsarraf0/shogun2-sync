// Package orchestrate wires config+paths+linkutil+conflicts together into
// the two user-facing actions this app has: Setup and Recover.
package orchestrate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"shogun2sync/internal/config"
	"shogun2sync/internal/conflicts"
	"shogun2sync/internal/linkutil"
	"shogun2sync/internal/paths"
)

// SyncTarget returns cfg's cloud root + sync subfolder, with ~ expanded.
func SyncTarget(cfg config.Config) string {
	root := paths.ExpandHome(cfg.CloudRoot)
	return filepath.Join(root, cfg.SyncSubfolder)
}

// present reports whether anything at all exists at p — including a
// symlink, which os.Stat would follow and Exists would miss if it dangled.
func present(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Lstat(p)
	return err == nil
}

// rememberedSavePath returns the save folder recorded by a previous
// successful setup, if it's still there.
//
// This takes priority over auto-detection because a player who pointed us
// at a non-default Steam library is a player DetectSavePath couldn't help
// in the first place — without this, every later Status check would look
// somewhere else and report a perfectly working setup as broken.
func rememberedSavePath(cfg config.Config) string {
	p := paths.ExpandHome(cfg.SavePath)
	if !present(p) {
		return ""
	}
	return p
}

type SetupResult struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	AlreadySet bool   `json:"alreadySet"`
	SavePath   string `json:"savePath"`
	SyncTarget string `json:"syncTarget"`
}

// Setup links savePath (the game's real save folder) into the cloud-synced
// target, moving any existing saves there first. savePath may be supplied
// by the caller (manual override) or left empty to use auto-detection.
func Setup(cfg config.Config, savePathOverride string) SetupResult {
	cloudRoot := paths.ExpandHome(cfg.CloudRoot)
	if !paths.Exists(cloudRoot) {
		return SetupResult{OK: false, Error: fmt.Sprintf(
			"Cloud folder '%s' doesn't exist. Make sure the cloud client is installed and has finished its first sync.", cloudRoot)}
	}

	savePath := savePathOverride
	if savePath == "" {
		savePath = rememberedSavePath(cfg)
	}
	if savePath == "" {
		savePath = paths.DetectSavePath()
	}
	if savePath == "" {
		return SetupResult{OK: false, Error: "Couldn't find the Shogun 2 save folder. Run the game once so it creates its save folder, then try again, or enter the path manually."}
	}

	target := SyncTarget(cfg)

	status, err := linkutil.Inspect(savePath, target)
	if err != nil {
		return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
	}

	if status.IsLink {
		if status.MatchesTarget {
			return SetupResult{OK: true, AlreadySet: true, SavePath: savePath, SyncTarget: target}
		}
		return SetupResult{OK: false, Error: fmt.Sprintf(
			"The save folder already links somewhere else (%s). Ask for help before continuing.", status.LinkTarget),
			SavePath: savePath, SyncTarget: target}
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
	}

	if status.Exists {
		if err := linkutil.MoveContents(savePath, target); err != nil {
			return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
		}
	}

	if err := linkutil.Link(savePath, target); err != nil {
		return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
	}

	return SetupResult{OK: true, SavePath: savePath, SyncTarget: target}
}

// Status reports the current setup state without changing anything, for
// the app's home/status view.
type StatusResult struct {
	ConfigExists bool   `json:"configExists"`
	SavePath     string `json:"savePath"`
	SyncTarget   string `json:"syncTarget"`
	Linked       bool   `json:"linked"`
	LinkedOK     bool   `json:"linkedOk"` // linked AND pointing at the right place
}

func Status(cfg config.Config) StatusResult {
	savePath := rememberedSavePath(cfg)
	if savePath == "" {
		savePath = paths.DetectSavePath()
	}
	if savePath == "" {
		savePath = paths.ExpectedSavePath()
	}
	target := SyncTarget(cfg)
	res := StatusResult{ConfigExists: config.Exists(), SavePath: savePath, SyncTarget: target}

	status, err := linkutil.Inspect(savePath, target)
	if err != nil {
		return res
	}
	res.Linked = status.IsLink
	res.LinkedOK = status.IsLink && status.MatchesTarget
	return res
}

// RecoverResult is what the Recover view shows: conflict files needing
// attention, or if there are none, a peek at recent saves so the player
// can confirm things look right.
type RecoverResult struct {
	OK        bool             `json:"ok"`
	Error     string           `json:"error,omitempty"`
	Conflicts []conflicts.File `json:"conflicts"`
	Recent    []conflicts.File `json:"recent,omitempty"`
}

func Recover(cfg config.Config) RecoverResult {
	target := SyncTarget(cfg)
	if !paths.Exists(target) {
		return RecoverResult{OK: false, Error: fmt.Sprintf("Sync folder not found at %s. Run setup first.", target)}
	}

	found, err := conflicts.Scan(target)
	if err != nil {
		return RecoverResult{OK: false, Error: err.Error()}
	}
	if len(found) > 0 {
		return RecoverResult{OK: true, Conflicts: found}
	}

	recent, err := conflicts.Recent(target, 10)
	if err != nil {
		return RecoverResult{OK: true, Conflicts: found}
	}
	return RecoverResult{OK: true, Conflicts: found, Recent: recent}
}

// ResolveConflict moves a conflict file out of the way into a hidden
// .trash subfolder (timestamped, so nothing is silently overwritten)
// rather than deleting it outright — a wrong click during a tense "which
// save do we keep" moment shouldn't be unrecoverable.
func ResolveConflict(path string) error {
	dir := filepath.Dir(path)
	trash := filepath.Join(dir, ".shogun2sync-trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s", time.Now().Unix(), filepath.Base(path))
	return os.Rename(path, filepath.Join(trash, name))
}
