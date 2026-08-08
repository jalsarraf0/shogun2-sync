// Package conflicts scans a synced save folder for the duplicate files
// Dropbox/OneDrive/Google Drive create when both players write at the same
// moment, so a desync can be resolved in minutes instead of derailing the
// campaign.
package conflicts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type File struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Modified int64  `json:"modified"` // unix seconds
}

// Scan lists files in dir whose name marks them as a cloud-sync conflict
// copy (Dropbox and OneDrive both include "conflicted copy" in the name;
// Google Drive's rclone-bisync uses ".conflict" suffixes). Sorted newest
// first, since that's usually what a player wants to look at.
func Scan(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		if !strings.Contains(lower, "conflict") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, File{
			Path:     filepath.Join(dir, e.Name()),
			Name:     e.Name(),
			Modified: info.ModTime().Unix(),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, nil
}

// Recent lists the n most recently modified files in dir, for the "looks
// clean, here's what's there" view when no conflicts are found.
func Recent(dir string, n int) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, File{
			Path:     filepath.Join(dir, e.Name()),
			Name:     e.Name(),
			Modified: info.ModTime().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}
