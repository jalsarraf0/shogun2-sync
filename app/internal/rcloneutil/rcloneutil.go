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
// the given name and attaches tokenJSON (from gdrive.Authorize) directly,
// so no interactive rclone auth step ever runs.
//
// rootFolderID may be empty: that's the "I own this Drive and I'm the one
// sharing it" case, where there's no shared folder to scope into — the
// remote just points at the user's own "My Drive" root as normal.
// clientID and clientSecret are recorded on the remote as well as being
// used for the login. rclone refreshes the access token itself on every
// later sync, and a refresh has to be made against the same OAuth client
// that issued the token — omit them and the token quietly stops
// refreshing once it expires, which looks like syncing "just stopping"
// an hour after setup.
func ConfigureGoogleDriveRemote(ctx context.Context, name, rootFolderID, tokenJSON, clientID, clientSecret string) error {
	if !Installed() {
		return ErrNotInstalled
	}
	exists, err := RemoteExists(ctx, name)
	if err != nil {
		return err
	}

	settings := [][2]string{
		{"scope", "drive"},
		{"token", tokenJSON},
		{"client_id", clientID},
		{"client_secret", clientSecret},
	}
	if rootFolderID != "" {
		settings = append(settings, [2]string{"root_folder_id", rootFolderID})
	}

	var args []string
	if exists {
		// `config update` takes key and value as separate arguments.
		args = []string{"config", "update", name}
		for _, kv := range settings {
			args = append(args, kv[0], kv[1])
		}
	} else {
		// `config create` takes them as key=value pairs.
		args = []string{"config", "create", name, "drive"}
		for _, kv := range settings {
			args = append(args, kv[0]+"="+kv[1])
		}
		args = append(args, "config_is_local=false")
	}
	args = append(args, "--non-interactive")

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

// ShareableLink returns a Drive share link for remoteName:subfolder, for
// the "I own this Drive" flow — the host needs something to actually send
// their friend after creating the sync folder.
func ShareableLink(ctx context.Context, remoteName, subfolder string) (string, error) {
	if !Installed() {
		return "", ErrNotInstalled
	}
	out, err := run(ctx, "link", remoteName+":"+subfolder)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
