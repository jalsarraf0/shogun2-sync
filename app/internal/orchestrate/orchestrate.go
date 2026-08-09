// Package orchestrate wires config+paths+linkutil+conflicts together into
// the two user-facing actions this app has: Setup and Recover.
package orchestrate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shogun2sync/internal/config"
	"shogun2sync/internal/conflicts"
	"shogun2sync/internal/linkutil"
	"shogun2sync/internal/paths"
)

// SyncTarget returns cfg's cloud root + sync subfolder, with ~ expanded.
//
// If the player already pointed CloudRoot at the sync folder itself (its
// basename is the configured subfolder name), that path is used as-is. Always
// joining would create CloudRoot/Shogun2SaveSync/Shogun2SaveSync on every
// re-setup — the nested-folder bug this app used to ship.
func SyncTarget(cfg config.Config) string {
	root := paths.ExpandHome(strings.TrimSpace(cfg.CloudRoot))
	name := strings.TrimSpace(cfg.SyncSubfolder)
	if name == "" {
		return root
	}
	if equalPathBase(root, name) {
		return root
	}
	return filepath.Join(root, name)
}

func equalPathBase(path, name string) bool {
	base := filepath.Base(filepath.Clean(path))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(base, name)
	}
	return base == name
}

// FlattenSameNameNesting collapses accidental root/name/name/... trees into a
// flat root. Prior releases re-created the sync folder name inside itself on
// every resync when the remote was already scoped to that folder; saves ended
// up several directories deep and the game never saw them.
//
// Every regular file under root/name is hoisted to root (collisions keep both
// copies via a setup-conflict rename). The nested directory tree is then
// removed. The save folder is supposed to be flat, so nothing useful is lost.
func FlattenSameNameNesting(root, name string) error {
	root = filepath.Clean(root)
	name = strings.TrimSpace(name)
	if root == "" || name == "" || name == "." || name == ".." {
		return nil
	}
	if strings.ContainsAny(name, `/\:`) || strings.Contains(name, string(filepath.Separator)) {
		return fmt.Errorf("refusing to flatten a path-like folder name %q", name)
	}
	nested := filepath.Join(root, name)
	info, err := os.Lstat(nested)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	var files []string
	err = filepath.WalkDir(nested, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// Only hoist real files. Symlinks/special nodes stay out of the way
		// rather than risk following them into the wrong place.
		if !d.Type().IsRegular() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("scanning nested sync folder %s: %w", nested, err)
	}
	for _, from := range files {
		to := nonClobberingSibling(filepath.Join(root, filepath.Base(from)))
		if err := moveFilePreserve(from, to); err != nil {
			return fmt.Errorf("hoisting %s: %w", filepath.Base(from), err)
		}
	}
	if err := os.RemoveAll(nested); err != nil {
		return fmt.Errorf("removing nested sync folder %s: %w", nested, err)
	}
	return nil
}

func nonClobberingSibling(want string) string {
	if _, err := os.Lstat(want); err != nil {
		return want
	}
	ext := filepath.Ext(want)
	base := strings.TrimSuffix(want, ext)
	stamp := time.Now().Format("2006-01-02 150405")
	for i := 0; ; i++ {
		candidate := fmt.Sprintf("%s (setup conflict %s)%s", base, stamp, ext)
		if i > 0 {
			candidate = fmt.Sprintf("%s (setup conflict %s-%d)%s", base, stamp, i, ext)
		}
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

func moveFilePreserve(from, to string) error {
	if equalPath(canonicalComparisonPath(from), canonicalComparisonPath(to)) {
		return nil
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(to)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(to)
		return closeErr
	}
	_ = os.Chtimes(to, info.ModTime(), info.ModTime())
	return os.Remove(from)
}

// ValidateConfig rejects ambiguous or dangerous paths before any save files
// are moved. The sync folder is deliberately one portable folder name (not a
// path): two players may use different operating systems against the same
// cloud folder, so names that are invalid on Windows are unsafe everywhere.
func ValidateConfig(cfg config.Config) error {
	switch cfg.CloudProvider {
	case "dropbox", "onedrive", "googledrive":
	default:
		return fmt.Errorf("choose Dropbox, OneDrive, or Google Drive")
	}
	root := paths.ExpandHome(strings.TrimSpace(cfg.CloudRoot))
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("choose an absolute path to the local cloud folder")
	}
	name := strings.TrimSpace(cfg.SyncSubfolder)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\<>:"|?*`) {
		return fmt.Errorf("sync folder name must be one portable folder name, not a path")
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("sync folder name cannot end in a dot or space")
	}
	reserved := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	for _, v := range []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"} {
		if reserved == v {
			return fmt.Errorf("%q is reserved by Windows; choose another sync folder name", name)
		}
	}
	return nil
}

// canonicalComparisonPath resolves as much of p as exists. This catches a
// cloud root reached through a symlink even before its sync subfolder exists.
func canonicalComparisonPath(p string) string {
	abs, err := filepath.Abs(paths.ExpandHome(p))
	if err != nil {
		return filepath.Clean(p)
	}
	probe := abs
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return filepath.Clean(abs)
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
}

func equalPath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func containedBy(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateSeparatePaths(savePath, target string) error {
	save := canonicalComparisonPath(savePath)
	sync := canonicalComparisonPath(target)
	if equalPath(save, sync) || containedBy(save, sync) || containedBy(sync, save) {
		return fmt.Errorf("the game save folder and sync folder must be separate, non-overlapping folders (%s and %s)", save, sync)
	}
	return nil
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
	if err := ValidateConfig(cfg); err != nil {
		return SetupResult{OK: false, Error: err.Error()}
	}
	cloudRoot := paths.ExpandHome(cfg.CloudRoot)
	if !paths.Exists(cloudRoot) {
		return SetupResult{OK: false, Error: fmt.Sprintf(
			"Cloud folder '%s' doesn't exist. Make sure the cloud client is installed and has finished its first sync.", cloudRoot)}
	}

	savePath := paths.ExpandHome(strings.TrimSpace(savePathOverride))
	if savePath == "" {
		savePath = rememberedSavePath(cfg)
	}
	if savePath == "" {
		savePath = paths.DetectSavePath()
	}
	if savePath == "" {
		return SetupResult{OK: false, Error: "Couldn't find the Shogun 2 save folder. Run the game once so it creates its save folder, then try again, or enter the path manually."}
	}
	if !strings.EqualFold(filepath.Base(filepath.Clean(savePath)), "save_games_multiplayer") {
		return SetupResult{OK: false, Error: fmt.Sprintf("Choose Shogun 2's save_games_multiplayer folder itself, not %s", savePath), SavePath: savePath}
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
	if err := validateSeparatePaths(savePath, target); err != nil {
		return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
	}
	// Undo nested Shogun2SaveSync/Shogun2SaveSync trees left by older builds
	// (or by re-creating the subfolder inside an already-scoped Drive root)
	// before linking the game at this path.
	if err := FlattenSameNameNesting(target, strings.TrimSpace(cfg.SyncSubfolder)); err != nil {
		return SetupResult{OK: false, Error: fmt.Sprintf("cleaning nested sync folders: %v", err), SavePath: savePath, SyncTarget: target}
	}

	// Prove that this account can create the required link before moving a
	// single save. If a Windows junction policy or directory permission blocks
	// links, setup fails with the original folder still completely intact.
	probe := fmt.Sprintf("%s.shogun2sync-link-test-%d", savePath, time.Now().UnixNano())
	if err := linkutil.Link(probe, target); err != nil {
		return SetupResult{OK: false, Error: fmt.Sprintf("checking whether the save folder can be linked safely: %v", err), SavePath: savePath, SyncTarget: target}
	}
	if err := linkutil.Unlink(probe); err != nil {
		return SetupResult{OK: false, Error: fmt.Sprintf("cleaning up the link safety check: %v", err), SavePath: savePath, SyncTarget: target}
	}

	if status.Exists {
		if err := linkutil.MoveContents(savePath, target); err != nil {
			return SetupResult{OK: false, Error: err.Error(), SavePath: savePath, SyncTarget: target}
		}
	}

	if err := linkutil.Link(savePath, target); err != nil {
		// A race or transient failure can still occur after the successful probe.
		// Restore a normal local folder so the game is never left without one;
		// the cloud copy remains untouched and recoverable.
		_ = os.MkdirAll(savePath, 0o755)
		restored, restoreErr := linkutil.CopyContents(target, savePath)
		if restoreErr != nil {
			return SetupResult{OK: false, Error: fmt.Sprintf("linking failed (%v). Your cloud saves remain safe in %s, but restoring the local folder also failed after %d entries: %v", err, target, restored, restoreErr), SavePath: savePath, SyncTarget: target}
		}
		return SetupResult{OK: false, Error: fmt.Sprintf("linking failed (%v), so setup restored a normal local save folder. No cloud files were deleted; close the game and try again", err), SavePath: savePath, SyncTarget: target}
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
	Error        string `json:"error,omitempty"`
}

func Status(cfg config.Config) StatusResult {
	if err := ValidateConfig(cfg); err != nil {
		return StatusResult{ConfigExists: config.Exists(), Error: err.Error()}
	}
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
		res.Error = fmt.Sprintf("checking the save-folder link: %v", err)
		return res
	}
	res.Linked = status.IsLink
	res.LinkedOK = status.IsLink && status.MatchesTarget
	return res
}

// UndoResult reports what Undo did, for the confirmation the player sees.
type UndoResult struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	SavePath string `json:"savePath"`
	Restored int    `json:"restored"`
}

// Undo puts things back the way they were: it removes the link at the save
// path and restores a real folder there holding a copy of the saves.
//
// The cloud folder is left completely untouched — the other player is still
// syncing against it, and their campaign shouldn't end because their friend
// decided to stop using this app. That also makes Undo safe to reach for
// when something looks wrong, which is the point: an action a player can
// reverse is one they'll actually be willing to try.
func Undo(cfg config.Config) UndoResult {
	savePath := rememberedSavePath(cfg)
	if savePath == "" {
		savePath = paths.DetectSavePath()
	}
	if savePath == "" {
		return UndoResult{Error: "Couldn't find the save folder, so there's nothing to undo."}
	}

	// Follow the link's own target rather than what config says it should
	// be: if those two ever disagree, the link is the truth, and copying
	// from the wrong folder would restore the wrong saves.
	st, err := linkutil.Inspect(savePath, "")
	if err != nil {
		return UndoResult{Error: err.Error(), SavePath: savePath}
	}
	if !st.IsLink {
		return UndoResult{Error: fmt.Sprintf(
			"%s isn't linked, so there's nothing to undo.", savePath), SavePath: savePath}
	}
	linked := st.LinkTarget

	// Build a complete replacement beside the link first. The live link stays
	// in place unless every save was copied successfully, so disk-full and
	// permission failures leave the game in its original working state.
	staging := fmt.Sprintf("%s.shogun2sync-restore-%d", savePath, time.Now().UnixNano())
	if err := os.Mkdir(staging, 0o755); err != nil {
		return UndoResult{Error: fmt.Sprintf("preparing a safe local restore: %v", err), SavePath: savePath}
	}
	restored, err := linkutil.CopyContents(linked, staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return UndoResult{
			Error:    fmt.Sprintf("copying the saves into a safe local staging folder failed: %v. The original link and saves in %s are unchanged.", err, linked),
			SavePath: savePath, Restored: restored,
		}
	}
	if err := linkutil.Unlink(savePath); err != nil {
		_ = os.RemoveAll(staging)
		return UndoResult{Error: fmt.Sprintf("removing the link after the safe copy completed: %v", err), SavePath: savePath}
	}
	if err := os.Rename(staging, savePath); err != nil {
		if relinkErr := linkutil.Link(savePath, linked); relinkErr == nil {
			_ = os.RemoveAll(staging)
			return UndoResult{Error: fmt.Sprintf("activating the restored folder failed: %v. The original link was put back and no cloud files were changed", err), SavePath: savePath}
		}
		return UndoResult{Error: fmt.Sprintf("activating the restored folder failed: %v. Your copied saves remain safe at %s and the cloud originals remain at %s", err, staging, linked), SavePath: savePath, Restored: restored}
	}
	return UndoResult{OK: true, SavePath: savePath, Restored: restored}
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
	if err := ValidateConfig(cfg); err != nil {
		return RecoverResult{OK: false, Error: err.Error()}
	}
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
func verifiedConflict(cfg config.Config, path string) (target, candidate string, match conflicts.File, err error) {
	if err := ValidateConfig(cfg); err != nil {
		return "", "", match, err
	}
	target = canonicalComparisonPath(SyncTarget(cfg))
	candidate = canonicalComparisonPath(path)
	if !containedBy(candidate, target) || !equalPath(filepath.Dir(candidate), target) {
		return "", "", match, fmt.Errorf("refusing to resolve a file outside the configured sync folder")
	}
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", "", match, fmt.Errorf("checking conflict file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", match, fmt.Errorf("the selected conflict is not a regular file")
	}
	found, err := conflicts.Scan(target)
	if err != nil {
		return "", "", match, err
	}
	for _, conflict := range found {
		if equalPath(canonicalComparisonPath(conflict.Path), candidate) {
			return target, candidate, conflict, nil
		}
	}
	return "", "", match, fmt.Errorf("the selected file is no longer a detected conflict; scan again before resolving it")
}

func trashDestination(trash, candidate string) (string, error) {
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), i, filepath.Base(candidate))
		destination := filepath.Join(trash, name)
		if _, err := os.Lstat(destination); os.IsNotExist(err) {
			return destination, nil
		}
	}
	return "", fmt.Errorf("could not reserve a recovery-trash name for %s", filepath.Base(candidate))
}

func ResolveConflict(cfg config.Config, path string) error {
	target, candidate, _, err := verifiedConflict(cfg, path)
	if err != nil {
		return err
	}
	trash := filepath.Join(target, ".shogun2sync-trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	destination, err := trashDestination(trash, candidate)
	if err != nil {
		return err
	}
	return os.Rename(candidate, destination)
}

// PromoteConflict keeps the selected conflict copy as the canonical save.
// The previous original is moved to recovery trash first, and the selected
// file remains untouched unless an exclusive replacement path is secured.
func PromoteConflict(cfg config.Config, path string) error {
	target, candidate, conflict, err := verifiedConflict(cfg, path)
	if err != nil {
		return err
	}
	if conflict.Original == "" || filepath.Base(conflict.Original) != conflict.Original {
		return fmt.Errorf("this conflict cannot be matched safely to an original save")
	}
	original := filepath.Join(target, conflict.Original)
	info, err := os.Lstat(original)
	if err != nil {
		return fmt.Errorf("checking original save: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("the matched original is not a regular file")
	}
	trash := filepath.Join(target, ".shogun2sync-trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	trashedOriginal, err := trashDestination(trash, original)
	if err != nil {
		return err
	}
	if err := os.Rename(original, trashedOriginal); err != nil {
		return fmt.Errorf("preserving the previous original: %w", err)
	}
	// Hard-link creation is exclusive: if a cloud client recreates the
	// original between the checks above, this fails instead of overwriting it.
	if err := os.Link(candidate, original); err != nil {
		if restoreErr := os.Rename(trashedOriginal, original); restoreErr != nil {
			return fmt.Errorf("promoting the selected copy failed (%v); the previous original remains safe at %s because restoring its name also failed: %v", err, trashedOriginal, restoreErr)
		}
		return fmt.Errorf("promoting the selected copy failed: %w; the previous original was restored", err)
	}
	if err := os.Remove(candidate); err != nil {
		return fmt.Errorf("the selected copy is now the original, but removing its old duplicate name failed: %w", err)
	}
	return nil
}
