// Package selfcheck answers "will this installed copy actually run?" without
// starting the GUI. Installers and CI call it after files land so a missing
// rclone, an unwritable config dir, or a broken private binary fails loudly
// instead of producing a window that cannot sync.
package selfcheck

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shogun2sync/internal/applog"
	"shogun2sync/internal/paths"
	"shogun2sync/internal/rcloneutil"
)

// Check is one line of the self-check report.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	// Fatal means a failure here will make the app unusable for its core job.
	// Non-fatal failures are reported but do not fail the overall exit status
	// (for example: the game has never been run, so no save folder exists yet).
	Fatal bool `json:"fatal"`
}

// Report is the full self-check outcome.
type Report struct {
	Checks []Check `json:"checks"`
	OK     bool    `json:"ok"`
}

// Run exercises every dependency the installers claim to ship, plus the
// host-owned pieces we deliberately leave to the machine.
func Run(ctx context.Context) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	var checks []Check

	checks = append(checks, checkExecutable())
	checks = append(checks, checkRclone(ctx)...)
	checks = append(checks, checkConfigDir())
	checks = append(checks, checkSavePath())

	ok := true
	for _, c := range checks {
		if c.Fatal && !c.OK {
			ok = false
		}
	}
	return Report{Checks: checks, OK: ok}
}

func checkExecutable() Check {
	exe, err := os.Executable()
	if err != nil {
		return Check{
			Name:   "application binary",
			OK:     false,
			Fatal:  true,
			Detail: fmt.Sprintf("cannot resolve own path: %v", err),
		}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return Check{
		Name:   "application binary",
		OK:     true,
		Fatal:  true,
		Detail: exe,
	}
}

func checkRclone(ctx context.Context) []Check {
	path, err := rcloneutil.Path()
	if err != nil {
		return []Check{{
			Name:   "rclone binary",
			OK:     false,
			Fatal:  true,
			Detail: "not found next to the app (or on PATH). Reinstall from the official package so rclone is bundled.",
		}}
	}

	// Bound the probe so a hung antivirus scan of rclone.exe cannot freeze
	// an unattended install check forever.
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, "version")
	out, runErr := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if runErr != nil {
		detail := text
		if detail == "" {
			detail = runErr.Error()
		}
		return []Check{
			{
				Name:   "rclone binary",
				OK:     true,
				Fatal:  true,
				Detail: path,
			},
			{
				Name:   "rclone runs",
				OK:     false,
				Fatal:  true,
				Detail: detail,
			},
		}
	}
	if !strings.Contains(text, "rclone v") {
		return []Check{
			{
				Name:   "rclone binary",
				OK:     true,
				Fatal:  true,
				Detail: path,
			},
			{
				Name:   "rclone runs",
				OK:     false,
				Fatal:  true,
				Detail: "version output did not look like rclone: " + firstLine(text),
			},
		}
	}

	// Prefer the private sibling over PATH: that is what every installer
	// ships, and what a clean machine has. PATH-only is still OK for dev.
	exe, _ := os.Executable()
	sibling := ""
	if exe != "" {
		sibling = filepath.Join(filepath.Dir(exe), rcloneSiblingName())
	}
	source := "PATH"
	if sibling != "" && sameFile(path, sibling) {
		source = "bundled beside application"
	} else if runtime.GOOS == "linux" && path == "/usr/lib/shogun2sync/rclone" {
		source = "native package (/usr/lib/shogun2sync/rclone)"
	}

	return []Check{
		{
			Name:   "rclone binary",
			OK:     true,
			Fatal:  true,
			Detail: fmt.Sprintf("%s (%s)", path, source),
		},
		{
			Name:   "rclone runs",
			OK:     true,
			Fatal:  true,
			Detail: firstLine(text),
		},
	}
}

func checkConfigDir() Check {
	dir, err := applog.Dir()
	if err != nil {
		return Check{
			Name:   "config/log directory",
			OK:     false,
			Fatal:  true,
			Detail: err.Error(),
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Check{
			Name:   "config/log directory",
			OK:     false,
			Fatal:  true,
			Detail: fmt.Sprintf("cannot create %s: %v", dir, err),
		}
	}
	probe := filepath.Join(dir, ".selfcheck-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return Check{
			Name:   "config/log directory",
			OK:     false,
			Fatal:  true,
			Detail: fmt.Sprintf("cannot write to %s: %v", dir, err),
		}
	}
	_ = os.Remove(probe)
	return Check{
		Name:   "config/log directory",
		OK:     true,
		Fatal:  true,
		Detail: dir,
	}
}

func checkSavePath() Check {
	if found := paths.DetectSavePath(); found != "" {
		return Check{
			Name:   "Shogun 2 multiplayer save folder",
			OK:     true,
			Fatal:  false,
			Detail: found,
		}
	}
	expected := paths.ExpectedSavePath()
	if expected == "" {
		return Check{
			Name:   "Shogun 2 multiplayer save folder",
			OK:     false,
			Fatal:  false,
			Detail: "not found (game may not be installed yet)",
		}
	}
	return Check{
		Name:   "Shogun 2 multiplayer save folder",
		OK:     false,
		Fatal:  false,
		Detail: "not found yet; expected at " + expected + " after the game has been run once",
	}
}

func rcloneSiblingName() string {
	if runtime.GOOS == "windows" {
		return "rclone.exe"
	}
	return "rclone"
}

func sameFile(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return ra == rb
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// Format returns a human-readable multi-line report suitable for a console
// or a MessageBox.
func Format(r Report) string {
	var b strings.Builder
	b.WriteString("Shogun 2 Save Sync — install self-check\n")
	b.WriteString(strings.Repeat("=", 40) + "\n")
	for _, c := range r.Checks {
		mark := "OK"
		if !c.OK {
			if c.Fatal {
				mark = "FAIL"
			} else {
				mark = "WARN"
			}
		}
		fmt.Fprintf(&b, "[%s] %s\n    %s\n", mark, c.Name, c.Detail)
	}
	if r.OK {
		b.WriteString("\nResult: ready to use.\n")
	} else {
		b.WriteString("\nResult: not ready — reinstall from the official release, or see the FAIL lines above.\n")
	}
	return b.String()
}
