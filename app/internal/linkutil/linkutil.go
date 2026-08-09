// Package linkutil moves an existing save folder into the cloud-synced
// location and replaces it with a link back, so the game keeps writing to
// the same path it always has while the cloud client mirrors the real
// folder to the other player.
package linkutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LinkStatus describes what, if anything, is already set up at savePath.
type LinkStatus struct {
	Exists        bool   // savePath exists at all (as a real dir or a link)
	IsLink        bool   // savePath is a symlink or junction
	LinkTarget    string // resolved target, if IsLink
	MatchesTarget bool   // LinkTarget already equals the intended sync target
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
		absTarget := canonicalPath(syncTarget)
		absLinkTarget := canonicalPath(st.LinkTarget)
		if runtime.GOOS == "windows" {
			st.MatchesTarget = strings.EqualFold(absTarget, absLinkTarget)
		} else {
			st.MatchesTarget = absTarget == absLinkTarget
		}
	}
	return st, nil
}

func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func inside(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func rejectOverlappingDirectories(src, dst string) error {
	src = canonicalPath(src)
	dst = canonicalPath(dst)
	if samePath(src, dst) || inside(src, dst) || inside(dst, src) {
		return fmt.Errorf("refusing to move overlapping folders: %s and %s", src, dst)
	}
	return nil
}

// MoveContents moves every entry from src into dst (both must already
// exist as real directories), then removes src.
//
// os.Rename on its own isn't enough: the game's save folder lives inside
// the Steam library, which is very often on a different drive than the
// cloud folder under $HOME, and rename across filesystems fails (EXDEV on
// Linux, ERROR_NOT_SAME_DEVICE on Windows). So a failed rename falls back
// to copy-then-delete rather than aborting setup.
func MoveContents(src, dst string) error {
	if err := rejectOverlappingDirectories(src, dst); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		if _, err := copyToAvailable(from, filepath.Join(dst, e.Name())); err != nil {
			return fmt.Errorf("moving %s safely: %w", e.Name(), err)
		}
		if rerr := os.RemoveAll(from); rerr != nil {
			return fmt.Errorf("removing %s after copying it: %w", e.Name(), rerr)
		}
	}
	return os.Remove(src)
}

// nonClobberingPath returns want, or a renamed variant if something is
// already there.
//
// The shared folder may already hold the other player's save under the same
// name — os.Rename would overwrite it silently on Linux (and fail outright
// on Windows), and destroying the other player's save during setup is the
// worst thing this app could possibly do. Keeping both is always
// recoverable. "conflict" is in the generated name deliberately: that's
// what the Recover view scans for, so the pair surfaces as something for
// the two players to settle, which is exactly what it is.
func nonClobberingPath(want string) string {
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

// CopyContents copies every entry from src into dst, leaving src alone,
// and reports how many entries it copied. Existing files in dst are kept
// and the incoming copy is renamed, same as MoveContents.
func CopyContents(src, dst string) (int, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if _, err := copyToAvailable(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return n, fmt.Errorf("copying %s: %w", e.Name(), err)
		}
		n++
	}
	return n, nil
}

// copyToAvailable closes the race between choosing a non-clobbering name and
// creating it. Cloud clients can materialise a file at any moment; exclusive
// creation makes that a retry instead of an overwrite.
func copyToAvailable(src, want string) (string, error) {
	for attempts := 0; attempts < 100; attempts++ {
		dst := nonClobberingPath(want)
		if err := copyPath(src, dst); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		return dst, nil
	}
	return "", fmt.Errorf("could not reserve a safe destination for %s", filepath.Base(want))
}

// Unlink removes the link at savePath without touching whatever it points
// at. Removing a link must never recurse into the cloud folder and delete
// the other player's saves, so this deliberately uses os.Remove — which
// unlinks a symlink and detaches a junction — and never os.RemoveAll.
func Unlink(savePath string) error {
	st, err := Inspect(savePath, "")
	if err != nil {
		return err
	}
	if !st.IsLink {
		return fmt.Errorf("%s is not a link", savePath)
	}
	return os.Remove(savePath)
}

// copyPath recursively copies src to dst, preserving permissions and
// modification times. Modification times matter here: the Recover view
// ranks saves by mtime, so a copy that stamped everything with "now" would
// destroy exactly the ordering a player needs during a desync.
func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)

	case info.IsDir():
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			return err
		}
		complete := false
		defer func() {
			if !complete {
				_ = os.RemoveAll(dst)
			}
		}()
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		if err := os.Chtimes(dst, time.Now(), info.ModTime()); err != nil {
			return err
		}
		complete = true
		return nil

	case info.Mode().IsRegular():
		if err := copyFile(src, dst, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chtimes(dst, time.Now(), info.ModTime())

	default:
		return fmt.Errorf("%s: unsupported file type %s", src, info.Mode().Type())
	}
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
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
		junctionErr := createJunction(savePath, target)
		if junctionErr == nil {
			return nil
		}
		// A symlink is the fallback, but it needs Administrator rights or
		// Developer Mode, so it usually fails too on a stock account. When
		// both fail, the junction error is the one worth reading — the
		// symlink's "a required privilege is not held" would just send
		// people hunting for the wrong fix.
		if err := os.Symlink(target, savePath); err != nil {
			return fmt.Errorf("could not link %s to %s: %w", savePath, target, junctionErr)
		}
		return nil
	}
	return os.Symlink(target, savePath)
}
