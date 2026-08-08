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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	ServiceName = "shogun2sync-gdrive-bisync"
)

// Available reports whether this mechanism applies on this OS at all.
func Available() bool {
	return runtime.GOOS == "linux"
}

// EnsureMirror establishes (or re-establishes) a bisync baseline between
// localDir and remoteName:remoteSubfolder, then installs and enables a
// systemd --user timer that keeps them in sync every two minutes.
func EnsureMirror(ctx context.Context, remoteName, remoteSubfolder, localDir string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("bisync is only needed on Linux")
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		return fmt.Errorf("rclone is not installed")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl is not available")
	}

	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", localDir, err)
	}

	remote := remoteName + ":" + remoteSubfolder

	baseline := exec.CommandContext(ctx, "rclone", "bisync", localDir, remote,
		"--resync", "--create-empty-src-dirs")
	if out, err := baseline.CombinedOutput(); err != nil {
		return fmt.Errorf("initial bisync failed: %w: %s", err, out)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	systemdDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		return err
	}

	rclonePath, err := exec.LookPath("rclone")
	if err != nil {
		return err
	}
	shPath, err := exec.LookPath("sh")
	if err != nil {
		return err
	}

	// rclone bisync refuses to run when its last-known ("prior") listing
	// has zero entries, treating that as indistinguishable from a mount
	// failure that emptied a folder that should have real content. For a
	// freshly set-up sync folder, zero entries is simply the truth, not a
	// failure — so on that specific error we fall back to --resync, which
	// is safe here precisely because there's nothing at risk yet. Once
	// real files exist this branch stops being taken at all.
	normalRun := fmt.Sprintf("%s bisync %q %q --resilient --recover --create-empty-src-dirs", rclonePath, localDir, remote)
	resyncRun := fmt.Sprintf("%s bisync %q %q --resync --create-empty-src-dirs", rclonePath, localDir, remote)

	serviceContents := fmt.Sprintf(`[Unit]
Description=Bisync Shogun2 save folder with Google Drive

[Service]
Type=oneshot
ExecStart=%s -c '%s || %s'
`, shPath, normalRun, resyncRun)

	timerContents := `[Unit]
Description=Run Shogun2 Google Drive bisync every 2 minutes

[Timer]
OnBootSec=1min
OnUnitActiveSec=2min
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

	steps := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "--now", ServiceName + ".timer"},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w: %s", args, err, out)
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
