// Package linkutil moves an existing save folder into the cloud-synced
// location and replaces it with a link back, so the game keeps writing to
// the same path it always has while the cloud client mirrors the real
// folder to the other player.
package linkutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// LinkStatus describes what, if anything, is already set up at savePath.
type LinkStatus struct {
	Exists       bool   // savePath exists at all (as a real dir or a link)
	IsLink       bool   // savePath is a symlink or junction
	LinkTarget   string // resolved target, if IsLink
	MatchesTarget bool  // LinkTarget already equals the intended sync target
}

// Inspect reports the current state of savePath relative to the intended
// syncTarget, without changing anything.
func Inspect(savePath, syncTarget string) (LinkStatus, error) {
	var st LinkStatus

	info, err := os.Lstat(savePath)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Exists = true

	if info.Mode()&os.ModeSymlink != 0 {
		st.IsLink = true
		target, err := os.Readlink(savePath)
		if err != nil {
			return st, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(savePath), target)
		}
		st.LinkTarget = target
	} else if runtime.GOOS == "windows" {
		// Windows junctions don't set ModeSymlink in Go's stdlib; detect via
		// the reparse point helper in windows.go.
		if target, ok := junctionTarget(savePath); ok {
			st.IsLink = true
			st.LinkTarget = target
		}
	}

	if st.IsLink {
		absTarget, _ := filepath.Abs(syncTarget)
		absLinkTarget, _ := filepath.Abs(st.LinkTarget)
		st.MatchesTarget = absTarget == absLinkTarget
	}
	return st, nil
}

// MoveContents moves every entry from src into dst (both must already
// exist as real directories), then removes src.
func MoveContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("moving %s: %w", e.Name(), err)
		}
	}
	return os.Remove(src)
}

// Link creates a link at savePath pointing at target. On Linux this is a
// plain symlink. On Windows it tries a directory junction first (works
// without admin rights or Developer Mode), falling back to a symbolic
// link if junction creation fails for some reason.
func Link(savePath, target string) error {
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err := createJunction(savePath, target); err == nil {
			return nil
		}
		// Fall back to a real symlink (needs admin or Developer Mode).
		return os.Symlink(target, savePath)
	}
	return os.Symlink(target, savePath)
}

// createJunction shells out to mklink /J, since Go's stdlib has no direct
// junction API and mklink handles the reparse-point plumbing correctly.
func createJunction(link, target string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("junctions are Windows-only")
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J failed: %w: %s", err, out)
	}
	return nil
}
