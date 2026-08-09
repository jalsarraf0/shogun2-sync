// Package bisync sets up a real local mirror directory kept in sync with a
// Google Drive folder via `rclone bisync` on a systemd user timer.
//
// This exists only because Google ships no native Linux sync client
// (unlike Dropbox and OneDrive, which stay out of this package entirely —
// they already maintain a real local folder themselves). A FUSE mount
// would be simpler to wire up, but it streams reads/writes over the
// network live, which is exactly the failure mode that makes Google
// Drive's own Windows "Streaming" mode unsafe for an actively-written
// save file. A real local directory, periodically reconciled, avoids that.
package bisync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"shogun2sync/internal/rcloneutil"
)

const (
	ServiceName = "shogun2sync-gdrive-bisync"

	// MinRcloneMajor/Minor is the oldest rclone that understands the flags
	// below. --recover, --max-lock, --resync-mode and the --conflict-*
	// family are all available together from rclone 1.71. On anything older the timer
	// would fail on every single run with "unknown flag", so this is
	// checked up front where we can still say something useful.
	MinRcloneMajor = 1
	MinRcloneMinor = 71
)

// Available reports whether this mechanism applies on this OS at all.
func Available() bool {
	return runtime.GOOS == "linux"
}

var versionRe = regexp.MustCompile(`v(\d+)\.(\d+)(?:\.(\d+))?`)

// Version returns the installed rclone's major and minor version.
func Version(ctx context.Context) (major, minor int, err error) {
	rclonePath, err := rcloneutil.Path()
	if err != nil {
		return 0, 0, err
	}
	out, err := exec.CommandContext(ctx, rclonePath, "version").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("running rclone version: %w", err)
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0, fmt.Errorf("could not read a version number out of %q", strings.TrimSpace(string(out)))
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, nil
}

// CheckVersion reports a friendly error if rclone is too old to drive.
func CheckVersion(ctx context.Context) error {
	major, minor, err := Version(ctx)
	if err != nil {
		return err
	}
	if major > MinRcloneMajor || (major == MinRcloneMajor && minor >= MinRcloneMinor) {
		return nil
	}
	return fmt.Errorf(
		"rclone %d.%d is too old for reliable syncing (need %d.%d or newer). "+
			"Update it with your package manager, or install the current version from https://rclone.org/install/",
		major, minor, MinRcloneMajor, MinRcloneMinor)
}

// shellQuote wraps s for safe use inside single quotes in the generated
// script. A path is player-chosen and can contain anything, apostrophes
// included.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// systemdQuote protects one ExecStart argument. Unit files have their own
// quoting rules: percent starts a specifier, dollar starts environment
// expansion, and spaces split arguments even though no shell is involved.
func systemdQuote(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		`%`, `%%`,
		`$`, `$$`,
	)
	return `"` + r.Replace(s) + `"`
}

// systemdConditionPath protects a path used as a Condition* value. Conditions
// are not command lines: systemd takes the rest of the line verbatim, so the
// ExecStart quoting rules do not apply and quoting a path actively breaks it
// ("path is not absolute"). Only the percent specifier needs escaping. An empty
// result means the caller should omit the condition rather than emit a unit
// systemd would reject.
func systemdConditionPath(path string) string {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\n\r") {
		return ""
	}
	return strings.ReplaceAll(path, `%`, `%%`)
}

func serviceUnit(rclonePath, shPath, scriptPath string) string {
	condition := ""
	// Package removal deletes the bundled rclone but cannot reach into a user's
	// systemd directory, so the leftover timer must refuse to run on its own.
	if guard := systemdConditionPath(rclonePath); guard != "" {
		condition = "ConditionFileIsExecutable=" + guard + "\n"
	}
	return fmt.Sprintf(`[Unit]
Description=Sync the Shogun 2 save folder with Google Drive
%s
[Service]
Type=oneshot
ExecStart=%s %s
`, condition, systemdQuote(shPath), systemdQuote(scriptPath))
}

// syncScript is the body of the script the timer runs.
//
// It's a real file rather than an inline `sh -c '...'` in the unit because
// that inline form has to survive two layers of quoting (systemd's, then
// the shell's), and gets both a percent sign and an apostrophe in a path
// wrong. A file has neither problem and is also something a player can
// read when they want to know what's running on their machine.
func syncScript(rclonePath, localDir, remote, logPath, syncFolderName, externalTrash string) string {
	if strings.TrimSpace(syncFolderName) == "" {
		syncFolderName = "Shogun2SaveSync"
	}
	if strings.TrimSpace(externalTrash) == "" {
		externalTrash = filepath.Join(filepath.Dir(logPath), "save-trash")
	}
	// Percent signs doubled for fmt.Sprintf (shell date/stat formats).
	return fmt.Sprintf(`#!/bin/sh
# Generated by Shogun 2 Save Sync. Edits will be overwritten.
set -u

RCLONE=%s
LOCAL=%s
REMOTE=%s
LOG=%s
SYNC_NAME=%s
TRASH=%s

# Keep support logs useful without allowing an unattended timer to grow one
# forever. One previous generation is enough for diagnosing a recurring job.
if [ -f "$LOG" ] && [ "$(wc -c < "$LOG")" -gt 1048576 ]; then
  rm -f "$LOG.1"
  mv "$LOG" "$LOG.1"
fi

# Collapse accidental LOCAL/SYNC_NAME/SYNC_NAME nesting left by older builds
# before every sync so the game and the other player always see a flat folder.
flatten_nested() {
  root=$1
  name=$2
  nested="$root/$name"
  [ -d "$nested" ] || return 0
  find "$nested" -type f -print0 2>/dev/null | while IFS= read -r -d '' f; do
    base=$(basename "$f")
    dest="$root/$base"
    if [ -e "$dest" ]; then
      stamp=$(date '+%%Y-%%m-%%d %%H%%M%%S')
      dest="$root/$base (setup conflict $stamp)"
    fi
    mv "$f" "$dest"
  done
  rm -rf "$nested"
}

flatten_nested "$LOCAL" "$SYNC_NAME"

# Cooperative lock so both players' 30s timers do not bisync at the same
# instant. Lives beside the log (never inside the save folder) so the game
# and bisync never see it. mkdir is atomic; stale locks older than 90s are
# stolen so a crashed client cannot block forever.
LOCK="$(dirname "$LOG")/sync.lock"
lock_now=$(date +%%s)
if [ -d "$LOCK" ]; then
  lock_age=$((lock_now - $(stat -c %%Y "$LOCK" 2>/dev/null || echo 0)))
  if [ "$lock_age" -lt 90 ]; then
    echo "another player is syncing (lock age ${lock_age}s); will try again shortly" >>"$LOG"
    exit 0
  fi
  rm -rf "$LOCK"
fi
if ! mkdir "$LOCK" 2>/dev/null; then
  echo "another player grabbed the sync lock; will try again shortly" >>"$LOG"
  exit 0
fi
release_lock() { rmdir "$LOCK" 2>/dev/null || rm -rf "$LOCK" 2>/dev/null; }
trap release_lock EXIT INT TERM

# Filter rules keep the lock/trash out of the shared folder if a prior build
# left them there, and ignore any other app-private dot dirs.
"$RCLONE" bisync "$LOCAL" "$REMOTE" \
  --resilient --recover --max-lock 2m \
  --conflict-resolve newer --conflict-loser num --conflict-suffix conflict \
  --create-empty-src-dirs --drive-skip-gdocs \
  --filter '- .shogun2sync-*/**' --filter '- .shogun2sync-*' \
  --filter '- .shogun2sync-sync.lock/**' --filter '- .shogun2sync-sync.lock' \
  --log-file "$LOG" --log-format date,time -v
status=$?

if [ "$status" -eq 0 ]; then
  # Keep only the 3 newest save files. Older turns go to an EXTERNAL trash
  # (outside the sync tree) and are deleted on the remote in the same step.
  # Trashing inside LOCAL would make the next bisync see a mass delete and
  # hit rclone's >50%% safety abort — which is exactly how a "keep 3" feature
  # would otherwise brick the timer after the first successful prune.
  mkdir -p "$TRASH"
  n=0
  find "$LOCAL" -maxdepth 1 -type f \( -name '*.save' -o -name '*.save_multiplayer' \) -printf '%%T@\t%%p\0' 2>/dev/null \
    | sort -nzr \
    | while IFS= read -r -d '' line; do
        path=$(printf '%%s' "$line" | cut -f2-)
        [ -f "$path" ] || continue
        n=$((n + 1))
        if [ "$n" -gt 3 ]; then
          base=$(basename "$path")
          # Delete on Drive first so the next bisync does not re-download it.
          "$RCLONE" deletefile "$REMOTE/$base" --drive-skip-gdocs 2>/dev/null || true
          mv "$path" "$TRASH/$(date +%%s)-$$-$base" 2>/dev/null || true
        fi
      done
  # Drop in-folder trash left by older builds so it cannot re-enter the mirror.
  if [ -d "$LOCAL/.shogun2sync-trash" ]; then
    mkdir -p "$TRASH"
    find "$LOCAL/.shogun2sync-trash" -type f -print0 2>/dev/null \
      | while IFS= read -r -d '' f; do
          mv "$f" "$TRASH/$(date +%%s)-$$-$(basename "$f")" 2>/dev/null || true
        done
    rm -rf "$LOCAL/.shogun2sync-trash"
  fi
  rm -rf "$LOCAL/.shogun2sync-sync.lock"
  exit 0
fi

# Exit 7 is a critical abort: rclone will refuse every later run until a
# --resync re-establishes the baseline.
#
# 1) Local empty: treat the remote as authoritative (path2). Common after a
#    mount dropout that made a legitimately empty folder look like a broken
#    baseline.
# 2) Local has files: an empty initial baseline leaves "empty prior Path1
#    listing" forever under --resilient alone. Re-baseline with newer wins
#    so the first real campaign save can upload without wiping the other
#    player's newer copy.
# Ignore leftover app-private dirs when deciding "empty".
if [ "$status" -eq 7 ]; then
  others=$(find "$LOCAL" -mindepth 1 -maxdepth 1 \
    ! -name '.shogun2sync-sync.lock' ! -name '.shogun2sync-trash' 2>/dev/null | head -n 1)
  release_lock
  trap - EXIT INT TERM
  if [ -z "$others" ]; then
    exec "$RCLONE" bisync "$LOCAL" "$REMOTE" --resync --resync-mode path2 \
      --create-empty-src-dirs \
      --filter '- .shogun2sync-*/**' --filter '- .shogun2sync-*' \
      --log-file "$LOG" --log-format date,time -v
  fi
  # Local has content: newer-wins resync, same flags as EstablishBaseline.
  exec "$RCLONE" bisync "$LOCAL" "$REMOTE" --resync --resync-mode newer \
    --conflict-resolve newer --conflict-loser num --conflict-suffix conflict \
    --create-empty-src-dirs \
    --filter '- .shogun2sync-*/**' --filter '- .shogun2sync-*' \
    --log-file "$LOG" --log-format date,time -v
fi

exit "$status"
`, shellQuote(rclonePath), shellQuote(localDir), shellQuote(remote), shellQuote(logPath), shellQuote(syncFolderName), shellQuote(externalTrash))
}

// Paths returns where the generated script and its log live.
func Paths() (scriptPath, logPath string, err error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(base, "shogun2sync")
	return filepath.Join(dir, "gdrive-bisync.sh"), filepath.Join(dir, "gdrive-bisync.log"), nil
}

// remoteSpec joins an rclone remote name with an optional subfolder into the
// path form bisync expects ("name:" or "name:Shogun2SaveSync").
func remoteSpec(remoteName, remoteSubfolder string) string {
	if remoteSubfolder == "" {
		return remoteName + ":"
	}
	return remoteName + ":" + remoteSubfolder
}

// EstablishBaseline creates localDir (if needed) and runs the initial
// rclone bisync --resync that production uses when a mirror pair is first
// set up. It does not install systemd units.
//
// CI uses this against a local-directory stand-in for Google Drive so the
// Linux Drive path is proven without OAuth or a real Drive account. The
// shipped app never exposes that stand-in to players.
func EstablishBaseline(ctx context.Context, remoteName, remoteSubfolder, localDir string, remoteRootIsSyncFolder bool) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("bisync is only needed on Linux")
	}
	rclonePath, err := rcloneutil.Path()
	if err != nil {
		return fmt.Errorf("rclone is not installed")
	}
	if err := CheckVersion(ctx); err != nil {
		return err
	}
	if remoteSubfolder == "" && !remoteRootIsSyncFolder {
		// Without this an empty subfolder would mirror the player's entire
		// Drive into a game-saves folder.
		return fmt.Errorf("refusing to mirror the whole Drive: no sync folder was given")
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", localDir, err)
	}
	remote := remoteSpec(remoteName, remoteSubfolder)
	// "newer" is deliberate. Bare --resync defaults to path1 and can
	// overwrite a newer save on Drive with an older local copy when a user
	// reconnects. Initialising a changed pair keeps the newest side and the
	// conflict flags preserve equal-time disagreements as visible copies.
	baseline := exec.CommandContext(ctx, rclonePath, "bisync", localDir, remote,
		"--resync", "--resync-mode", "newer",
		"--conflict-resolve", "newer", "--conflict-loser", "num",
		"--conflict-suffix", "conflict", "--create-empty-src-dirs")
	if out, err := baseline.CombinedOutput(); err != nil {
		return fmt.Errorf("initial sync failed: %w: %s", err, out)
	}
	return nil
}

// SyncOnce runs a single production-flagged bisync pass. The systemd timer
// script is the normal path for players; this exists so CI can prove the
// same flags move a save from one side of the mirror to the other without
// waiting on a timer.
//
// When the initial baseline was empty, rclone bisync can critical-abort the
// first real sync with "empty prior Path1 listing" even though the local
// folder now has saves. That is exit 7 and is not fixed by --resilient alone.
// Matching the timer script, we re-baseline with --resync-mode newer when
// local still has files so the first campaign save is not stuck forever.
func SyncOnce(ctx context.Context, remoteName, remoteSubfolder, localDir string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("bisync is only needed on Linux")
	}
	rclonePath, err := rcloneutil.Path()
	if err != nil {
		return fmt.Errorf("rclone is not installed")
	}
	remote := remoteSpec(remoteName, remoteSubfolder)
	cmd := exec.CommandContext(ctx, rclonePath, "bisync", localDir, remote,
		"--resilient", "--recover", "--max-lock", "2m",
		"--conflict-resolve", "newer", "--conflict-loser", "num",
		"--conflict-suffix", "conflict",
		"--create-empty-src-dirs", "--drive-skip-gdocs")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if exitCode(err) == 7 && dirHasRegularFiles(localDir) {
		if baseErr := EstablishBaseline(ctx, remoteName, remoteSubfolder, localDir, remoteSubfolder == ""); baseErr != nil {
			return fmt.Errorf("sync once failed: %w: %s; resync recovery also failed: %v", err, out, baseErr)
		}
		return nil
	}
	return fmt.Errorf("sync once failed: %w: %s", err, out)
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func dirHasRegularFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			return true
		}
	}
	return false
}

// EnsureMirror establishes (or re-establishes) a bisync baseline between
// localDir and remoteName:remoteSubfolder, then installs the systemd user
// units that will keep them in sync. It deliberately does not enable the
// timer: browser authorization happens before the player's final confirmation,
// so RunSetup activates the timer only after the save-folder link succeeds.
// remoteRootIsSyncFolder must only be true when the remote itself is scoped to
// the shared save folder; that explicit flag prevents an accidental empty
// subfolder from mirroring the player's entire Drive.
func EnsureMirror(ctx context.Context, remoteName, remoteSubfolder, localDir string, remoteRootIsSyncFolder bool) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("bisync is only needed on Linux")
	}
	rclonePath, err := rcloneutil.Path()
	if err != nil {
		return fmt.Errorf("rclone is not installed")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl is not available")
	}
	if err := CheckVersion(ctx); err != nil {
		return err
	}
	if remoteSubfolder == "" && !remoteRootIsSyncFolder {
		// Without this an empty subfolder would mirror the player's entire
		// Drive into a game-saves folder.
		return fmt.Errorf("refusing to mirror the whole Drive: no sync folder was given")
	}

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", localDir, err)
	}

	scriptPath, logPath, err := Paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		return err
	}
	remote := remoteSpec(remoteName, remoteSubfolder)
	syncFolderName := filepath.Base(filepath.Clean(localDir))
	// Trash lives next to the log, never inside the shared save folder.
	externalTrash := filepath.Join(filepath.Dir(logPath), "save-trash")
	desiredScript := syncScript(rclonePath, localDir, remote, logPath, syncFolderName, externalTrash)
	currentScript, _ := os.ReadFile(scriptPath)
	// Script flags and the bundled rclone path can change during an app
	// update without changing the mirror pair. Reinitialising merely because
	// the generated script improved would perform an unnecessary resync.
	current := string(currentScript)
	alreadyInitialised := strings.Contains(current, "\nLOCAL="+shellQuote(localDir)+"\n") &&
		strings.Contains(current, "\nREMOTE="+shellQuote(remote)+"\n")
	if !alreadyInitialised {
		if err := EstablishBaseline(ctx, remoteName, remoteSubfolder, localDir, remoteRootIsSyncFolder); err != nil {
			return err
		}
	}
	if err := os.WriteFile(scriptPath, []byte(desiredScript), 0o755); err != nil {
		return fmt.Errorf("writing %s: %w", scriptPath, err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	systemdDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		return err
	}

	shPath, err := exec.LookPath("sh")
	if err != nil {
		return err
	}

	serviceContents := serviceUnit(rclonePath, shPath, scriptPath)

	// 30s keeps turn-based multiplayer snappy: each player saves, the other
	// picks it up within one timer tick. AccuracySec lets systemd coalesce
	// wakeups; the script's shared lock prevents both machines bisyncing at
	// the exact same moment (they effectively alternate ticks).
	timerContents := `[Unit]
Description=Run the Shogun 2 Google Drive sync every 30 seconds

[Timer]
OnBootSec=15s
OnUnitActiveSec=30s
AccuracySec=1s
Persistent=true

[Install]
WantedBy=timers.target
`

	servicePath := filepath.Join(systemdDir, ServiceName+".service")
	timerPath := filepath.Join(systemdDir, ServiceName+".timer")

	if err := os.WriteFile(servicePath, []byte(serviceContents), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(timerPath, []byte(timerContents), 0o644); err != nil {
		return err
	}

	args := []string{"systemctl", "--user", "daemon-reload"}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %w: %s", args, err, out)
	}

	return nil
}

// EnableMirror activates the prepared timer after setup's filesystem changes
// have completed successfully. Keeping this separate prevents a cancelled
// first-run wizard from leaving an undisclosed background job behind.
func EnableMirror(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "enable", "--now", ServiceName+".timer")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting Google Drive background timer: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DisableMirror stops the Linux background job without touching either side
// of the mirror. It is intentionally idempotent so Undo stays safe even when
// setup only got partway through.
func DisableMirror(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	enabled, active := TimerStatus(ctx)
	if enabled || active {
		cmd := exec.CommandContext(ctx, "systemctl", "--user", "disable", "--now", ServiceName+".timer")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("stopping Google Drive background timer: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	serviceOut, _ := exec.CommandContext(ctx, "systemctl", "--user", "is-active", ServiceName+".service").CombinedOutput()
	if trimEq(serviceOut, "active") || trimEq(serviceOut, "activating") {
		cmd := exec.CommandContext(ctx, "systemctl", "--user", "stop", ServiceName+".service")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("waiting for the active Google Drive sync to stop: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// TimerStatus reports whether the sync timer is currently enabled and
// active, for the status view.
func TimerStatus(ctx context.Context) (enabled bool, active bool) {
	if runtime.GOOS != "linux" {
		return false, false
	}
	enabledOut, _ := exec.CommandContext(ctx, "systemctl", "--user", "is-enabled", ServiceName+".timer").CombinedOutput()
	activeOut, _ := exec.CommandContext(ctx, "systemctl", "--user", "is-active", ServiceName+".timer").CombinedOutput()
	return trimEq(enabledOut, "enabled"), trimEq(activeOut, "active")
}

// LastError returns a description of the last sync failure, or "" if the
// last run was fine. A player whose saves silently stopped syncing needs
// to be told, not left to notice on their own mid-campaign.
func LastError(ctx context.Context) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", ServiceName+".service",
		"--property=ExecMainStatus", "--value").CombinedOutput()
	if err != nil {
		return ""
	}
	code, convErr := strconv.Atoi(strings.TrimSpace(string(out)))
	if convErr != nil || code == 0 {
		return ""
	}
	// Exit codes are rclone bisync's, documented at
	// https://rclone.org/bisync/#exit-codes
	switch code {
	case 7:
		return "The background sync hit a problem it can't recover from on its own. " +
			"Re-run Setup to rebuild the sync, then check both players' saves."
	case 1:
		return "The last background sync didn't finish. It will try again shortly; " +
			"if this keeps showing, check the log."
	default:
		return fmt.Sprintf("The background sync exited with code %d. Check the log for details.", code)
	}
}

func trimEq(b []byte, want string) bool {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s == want
}

// LastSyncTime returns when the bisync service last ran successfully, for
// display ("last synced 2 minutes ago"). Returns zero time if unknown.
func LastSyncTime(ctx context.Context) time.Time {
	statusOut, err := exec.CommandContext(ctx, "systemctl", "--user", "show", ServiceName+".service",
		"--property=ExecMainStatus", "--value").CombinedOutput()
	if err != nil || !trimEq(statusOut, "0") {
		return time.Time{}
	}
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "show", ServiceName+".service",
		"--property=ExecMainExitTimestamp", "--value").CombinedOutput()
	if err != nil {
		return time.Time{}
	}
	s := string(out)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", s)
	if err != nil {
		return time.Time{}
	}
	return t
}
