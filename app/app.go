package main

import (
	"context"
	"fmt"
	"os/exec"
	goruntime "runtime"

	"shogun2sync/internal/bisync"
	"shogun2sync/internal/config"
	"shogun2sync/internal/gdrive"
	"shogun2sync/internal/orchestrate"
	"shogun2sync/internal/paths"
	"shogun2sync/internal/rcloneutil"

	runtime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// openForOAuth opens url specifically in Firefox when available, instead
// of the system default browser. This exists because Chromium-based
// browsers (Brave confirmed; likely Chrome/Edge too) can silently upgrade
// a plain http:// loopback redirect to https://, which our local OAuth
// callback server can't answer (it's not a TLS server) — surfacing as an
// inexplicable "connection refused" right after a successful Google
// login. Firefox doesn't do this by default. Falls back to the system
// default browser if Firefox isn't installed.
func openForOAuth(ctx context.Context, url string) error {
	if goruntime.GOOS == "linux" {
		if path, err := exec.LookPath("firefox"); err == nil {
			return exec.Command(path, url).Start()
		}
	}
	runtime.BrowserOpenURL(ctx, url)
	return nil
}

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// GoogleDriveAuthResult is what the frontend gets back after a successful
// Google Drive authorization.
type GoogleDriveAuthResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Account   string `json:"account,omitempty"`
	ShareLink string `json:"shareLink,omitempty"` // set when rootFolderID was empty (host flow)
}

// AuthorizeGoogleDrive runs the OAuth loopback flow (see internal/gdrive),
// then writes the resulting token into an rclone remote named "gdrive"
// and creates the sync subfolder inside it.
//
// rootFolderID is the folder ID from a link a friend shared with you.
// Leave it empty if you're the one whose Drive is being shared — the
// subfolder gets created in your own "My Drive" root instead, and a
// shareable link for it comes back in the result so you can send it on.
func (a *App) AuthorizeGoogleDrive(rootFolderID, subfolder string) GoogleDriveAuthResult {
	if !rcloneutil.Installed() {
		return GoogleDriveAuthResult{OK: false, Error: "rclone is not installed"}
	}

	result, err := gdrive.Authorize(a.ctx, func(url string) error {
		return openForOAuth(a.ctx, url)
	})
	if err != nil {
		return GoogleDriveAuthResult{OK: false, Error: err.Error()}
	}

	const remoteName = "gdrive"
	if err := rcloneutil.ConfigureGoogleDriveRemote(a.ctx, remoteName, rootFolderID, result.TokenJSON); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("saving rclone config: %v", err)}
	}
	if err := rcloneutil.VerifyAccess(a.ctx, remoteName); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("could not access the shared folder: %v", err)}
	}
	if subfolder != "" {
		if err := rcloneutil.EnsureSubfolder(a.ctx, remoteName, subfolder); err != nil {
			return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("creating sync folder: %v", err)}
		}
	}

	if bisync.Available() && subfolder != "" {
		// subfolder is never actually empty via the wizard (it defaults to
		// "Shogun2SaveSync"), but this guard matters: without it, an empty
		// subfolder would make bisync mirror the user's entire Drive root,
		// not just a game-saves folder.
		localDir := paths.DefaultCloudRoot("googledrive")
		if err := bisync.EnsureMirror(a.ctx, remoteName, subfolder, localDir); err != nil {
			return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("setting up local sync mirror: %v", err)}
		}
	}

	res := GoogleDriveAuthResult{OK: true}
	if rootFolderID == "" && subfolder != "" {
		if link, err := rcloneutil.ShareableLink(a.ctx, remoteName, subfolder); err == nil {
			res.ShareLink = link
		}
	}
	return res
}

// GoogleDriveMirrorStatus reports the Linux bisync timer's state, for the
// status view (Windows/macOS use the native Google Drive Desktop client
// instead and don't need this).
type GoogleDriveMirrorStatus struct {
	Applicable bool   `json:"applicable"`
	Enabled    bool   `json:"enabled"`
	Active     bool   `json:"active"`
	LastSync   string `json:"lastSync,omitempty"`
}

func (a *App) GetGoogleDriveMirrorStatus() GoogleDriveMirrorStatus {
	if !bisync.Available() {
		return GoogleDriveMirrorStatus{Applicable: false}
	}
	enabled, active := bisync.TimerStatus(a.ctx)
	last := bisync.LastSyncTime(a.ctx)
	res := GoogleDriveMirrorStatus{Applicable: true, Enabled: enabled, Active: active}
	if !last.IsZero() {
		res.LastSync = last.Format("2006-01-02 15:04:05")
	}
	return res
}

// ---- Config ----

func (a *App) ConfigExists() bool {
	return config.Exists()
}

func (a *App) GetConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}
	}
	return cfg
}

func (a *App) SaveConfigCmd(cfg config.Config) string {
	if err := config.Save(cfg); err != nil {
		return err.Error()
	}
	return ""
}

// ---- Path detection ----

func (a *App) DetectSavePath() string {
	return paths.DetectSavePath()
}

func (a *App) ExpectedSavePath() string {
	return paths.ExpectedSavePath()
}

func (a *App) DefaultCloudRoot(provider string) string {
	return paths.DefaultCloudRoot(provider)
}

func (a *App) PathExists(p string) bool {
	return paths.Exists(paths.ExpandHome(p))
}

// BrowseForFolder opens a native folder picker and returns the chosen
// path, or "" if the user cancelled.
func (a *App) BrowseForFolder(title string) string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil {
		return ""
	}
	return dir
}

// ---- Setup / Status / Recover ----

func (a *App) RunSetup(cfg config.Config, savePathOverride string) orchestrate.SetupResult {
	return orchestrate.Setup(cfg, savePathOverride)
}

func (a *App) GetStatus() orchestrate.StatusResult {
	cfg, _ := config.Load()
	return orchestrate.Status(cfg)
}

func (a *App) RunRecover() orchestrate.RecoverResult {
	cfg, _ := config.Load()
	return orchestrate.Recover(cfg)
}

// ResolveConflict moves a conflict file into a recoverable .trash folder.
// Returns "" on success or an error message.
func (a *App) ResolveConflict(path string) string {
	if err := orchestrate.ResolveConflict(path); err != nil {
		return err.Error()
	}
	return ""
}

// OpenInFileManager opens path in the OS's native file browser, for
// players who'd rather look at the folder themselves.
func (a *App) OpenInFileManager(path string) {
	runtime.BrowserOpenURL(a.ctx, "file://"+path)
}
