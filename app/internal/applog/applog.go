// Package applog writes the app's log to a file as well as the terminal.
//
// This exists because the two ways this app actually gets launched — a
// desktop icon on Linux, a double-clicked .exe on Windows — both discard
// anything written to stderr. When a player reports "it just didn't work",
// the log file is the only thing that can turn that into a diagnosis.
package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// maxSize caps the log at a size that's still trivial to attach to a bug
// report. On reaching it the file is rotated once, so there's always at
// most one previous run's worth of history alongside the current one.
const maxSize = 1 << 20 // 1 MiB

// Dir returns the directory holding the log file. It sits next to the
// config so there's a single folder to point people at.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "shogun2sync"), nil
}

// Path returns the log file's full path.
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "shogun2sync.log"), nil
}

// Init points the standard logger at both stderr and the log file. The
// returned function closes the file and should be deferred from main.
//
// Every failure here is deliberately non-fatal: not being able to write a
// log is never a good reason to stop a player from syncing their saves.
func Init() func() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)

	p, err := Path()
	if err != nil {
		log.Printf("applog: no config dir, logging to stderr only: %v", err)
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.Printf("applog: cannot create %s, logging to stderr only: %v", filepath.Dir(p), err)
		return func() {}
	}
	rotate(p)

	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("applog: cannot open %s, logging to stderr only: %v", p, err)
		return func() {}
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("--- shogun2sync starting ---")
	return func() { _ = f.Close() }
}

// rotate moves the log aside once it passes maxSize, keeping exactly one
// previous file. Any error just means we keep appending, which is fine.
func rotate(p string) {
	info, err := os.Stat(p)
	if err != nil || info.Size() < maxSize {
		return
	}
	_ = os.Rename(p, p+".1")
}

// Printf logs to the shared logger. It's a thin wrapper so callers don't
// each have to import "log" alongside this package.
func Printf(format string, args ...any) {
	log.Printf(format, args...)
}

// Tail returns the last n bytes of the log, for showing in-app so a player
// can copy it without hunting through their filesystem.
func Tail(n int64) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > n {
		if _, err := f.Seek(info.Size()-n, io.SeekStart); err != nil {
			return "", err
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n\n(log file: %s)", b, p), nil
}
