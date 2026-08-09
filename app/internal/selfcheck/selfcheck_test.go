package selfcheck

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunFindsDevRcloneOrReportsClearly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	report := Run(ctx)
	if len(report.Checks) < 3 {
		t.Fatalf("expected several checks, got %d", len(report.Checks))
	}

	// Config dir must always be creatable in tests.
	var sawConfig bool
	for _, c := range report.Checks {
		if c.Name == "config/log directory" {
			sawConfig = true
			if !c.OK {
				t.Fatalf("config/log directory failed: %s", c.Detail)
			}
		}
	}
	if !sawConfig {
		t.Fatal("missing config/log directory check")
	}

	// Format must be stable enough to show a player.
	text := Format(report)
	if !strings.Contains(text, "self-check") {
		t.Fatalf("format missing header: %q", text)
	}
	if report.OK && !strings.Contains(text, "ready to use") {
		t.Fatalf("successful report missing ready line: %q", text)
	}
}

func TestSameFile(t *testing.T) {
	if !sameFile("/a/b", "/a/b") {
		t.Fatal("identical paths should match")
	}
	if sameFile("/a/b", "/a/c") {
		t.Fatal("different paths should not match")
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("one\ntwo\n"); got != "one" {
		t.Fatalf("got %q", got)
	}
}

// TestFormatFailurePath keeps the FAIL wording pinned so install scripts that
// grep the self-check output do not break silently.
func TestFormatFailurePath(t *testing.T) {
	text := Format(Report{
		OK: false,
		Checks: []Check{
			{Name: "rclone binary", OK: false, Fatal: true, Detail: "missing"},
			{Name: "save folder", OK: false, Fatal: false, Detail: "not yet"},
		},
	})
	if !strings.Contains(text, "[FAIL] rclone binary") {
		t.Fatalf("missing FAIL line: %q", text)
	}
	if !strings.Contains(text, "[WARN] save folder") {
		t.Fatalf("missing WARN line: %q", text)
	}
	if !strings.Contains(text, "not ready") {
		t.Fatalf("missing not-ready summary: %q", text)
	}
}

func TestRcloneSiblingName(t *testing.T) {
	name := rcloneSiblingName()
	if runtime.GOOS == "windows" {
		if name != "rclone.exe" {
			t.Fatalf("got %q", name)
		}
		return
	}
	if name != "rclone" {
		t.Fatalf("got %q", name)
	}
}

func TestCheckConfigDirUsesRealLocation(t *testing.T) {
	c := checkConfigDir()
	if !c.OK {
		t.Fatalf("config dir check failed: %s", c.Detail)
	}
	if !filepath.IsAbs(c.Detail) {
		t.Fatalf("expected absolute path, got %q", c.Detail)
	}
	if _, err := os.Stat(c.Detail); err != nil {
		t.Fatalf("config dir missing after check: %v", err)
	}
}
