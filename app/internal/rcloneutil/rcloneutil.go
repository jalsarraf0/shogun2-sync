// Package rcloneutil drives the rclone CLI as a subprocess for the pieces
// we don't reimplement ourselves: the actual Drive API calls and the
// bisync engine. We only replace the OAuth handoff (see internal/gdrive),
// which is what was actually unreliable.
package rcloneutil

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotInstalled is returned when the rclone binary can't be found.
var ErrNotInstalled = fmt.Errorf("rclone is not installed")

// Installed reports whether the rclone binary is on PATH.
func Installed() bool {
	_, err := exec.LookPath("rclone")
	return err == nil
}

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "rclone", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rclone %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// RemoteExists reports whether a remote with this name is already configured.
func RemoteExists(ctx context.Context, name string) (bool, error) {
	if !Installed() {
		return false, ErrNotInstalled
	}
	out, err := run(ctx, "listremotes")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSuffix(strings.TrimSpace(line), ":") == name {
			return true, nil
		}
	}
	return false, nil
}

// ConfigureGoogleDriveRemote creates or updates a Google Drive remote with
// the given name, scoping it to rootFolderID and attaching tokenJSON (from
// gdrive.Authorize) directly, so no interactive rclone auth step ever runs.
func ConfigureGoogleDriveRemote(ctx context.Context, name, rootFolderID, tokenJSON string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	exists, err := RemoteExists(ctx, name)
	if err != nil {
		return err
	}
	args := []string{
		"config", "create", name, "drive",
		"scope=drive",
		"root_folder_id=" + rootFolderID,
		"token=" + tokenJSON,
		"config_is_local=false",
		"--non-interactive",
	}
	if exists {
		args = []string{
			"config", "update", name,
			"scope", "drive",
			"root_folder_id", rootFolderID,
			"token", tokenJSON,
			"--non-interactive",
		}
	}
	_, err = run(ctx, args...)
	return err
}

// EnsureSubfolder creates subfolder at the root of remote if it doesn't
// already exist. rclone mkdir is idempotent.
func EnsureSubfolder(ctx context.Context, remoteName, subfolder string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	_, err := run(ctx, "mkdir", remoteName+":"+subfolder)
	return err
}

// VerifyAccess does a cheap round-trip against the Drive API (listing the
// configured root) to confirm the token and root_folder_id actually work,
// so setup fails loudly here instead of silently later during sync.
func VerifyAccess(ctx context.Context, remoteName string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	_, err := run(ctx, "lsd", remoteName+":", "--max-depth", "1")
	return err
}
