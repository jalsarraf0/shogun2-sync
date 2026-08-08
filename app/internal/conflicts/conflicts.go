// Package conflicts scans a synced save folder for the duplicate files
// Dropbox/OneDrive/Google Drive create when both players write at the same
// moment, so a desync can be resolved in minutes instead of derailing the
// campaign.
//
// The three clients name those duplicates completely differently, and only
// one of them uses the word "conflict":
//
//	Dropbox      Otomo Spring 1545 (Ken's conflicted copy 2026-08-07).save
//	OneDrive     Otomo Spring 1545-DESKTOP-4KJ9P2A.save
//	Google Drive Otomo Spring 1545 (1).save
//	rclone       Otomo Spring 1545.save.conflict1
//
// So matching on the word alone — which is what this package used to do —
// finds Dropbox's duplicates and silently misses everyone else's, which is
// the entire feature for two thirds of users.
//
// The two quieter patterns (a trailing device name, a trailing number) are
// shapes a player could plausibly have typed themselves, so those are only
// treated as duplicates when the file they'd be a copy *of* is sitting
// right there next to them. That keeps "Otomo Spring 1545-final.save" from
// being called a conflict unless "Otomo Spring 1545.save" also exists.
package conflicts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type File struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Modified int64  `json:"modified"` // unix seconds
	// Reason is shown in the UI so the player can see why a file was
	// flagged and overrule us when a heuristic gets it wrong.
	Reason string `json:"reason,omitempty"`
	// Original is the file this one appears to be a duplicate of, when we
	// found it alongside. Empty when the name alone was conclusive.
	Original string `json:"original,omitempty"`
}

var (
	// Dropbox, and our own setup-time renames. Dropbox has several
	// variants ("conflicted copy", "Case Conflict", "Selective Sync
	// Conflict"), all of which contain the word.
	wordConflict = regexp.MustCompile(`(?i)conflict`)

	// rclone bisync appends its suffix after the extension:
	// "name.save.conflict1", or the pre-1.66 "name.save..path1".
	rcloneSuffix = regexp.MustCompile(`(?i)\.{1,2}(conflict|path)\d*$`)

	// Google Drive for desktop, and generic collision copies: " (1)".
	trailingNumber = regexp.MustCompile(`\s*\(\d+\)$`)

	// OneDrive appends the device name: "-DESKTOP-4KJ9P2A".
	//
	// Restricted to upper case on purpose. Windows computer names are
	// conventionally upper case, and without that restriction this also
	// matches a save the player named themselves — "Otomo 1545-final.save"
	// sitting next to "Otomo 1545.save" is not a sync conflict, it's
	// someone keeping two saves.
	trailingDevice = regexp.MustCompile(`-[A-Z0-9][A-Z0-9-]*$`)
)

// Scan lists files in dir that look like cloud-sync duplicates, newest
// first — that's usually the one a player wants to look at.
func Scan(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names[e.Name()] = true
		}
	}

	var out []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		reason, original := classify(e.Name(), names)
		if reason == "" {
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
			Reason:   reason,
			Original: original,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, nil
}

// classify decides whether name looks like a duplicate, returning a
// human-readable reason (empty if it doesn't) and the file it appears to
// duplicate, when a sibling was what gave it away.
func classify(name string, siblings map[string]bool) (reason, original string) {
	if wordConflict.MatchString(name) {
		return "Named as a conflicted copy", ""
	}

	// rclone's suffix sits after the extension, so check it before
	// splitting one off.
	if trimmed := rcloneSuffix.ReplaceAllString(name, ""); trimmed != name {
		if siblings[trimmed] {
			return "Duplicate left by the background sync", trimmed
		}
		return "Duplicate left by the background sync", ""
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)

	if trimmed := trailingNumber.ReplaceAllString(stem, ""); trimmed != stem {
		if orig := trimmed + ext; siblings[orig] {
			return "Looks like a second copy of the same save", orig
		}
	}

	if trimmed := trailingDevice.ReplaceAllString(stem, ""); trimmed != stem {
		if orig := trimmed + ext; siblings[orig] {
			return "Looks like the other computer's copy", orig
		}
	}

	return "", ""
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
