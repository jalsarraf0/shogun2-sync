package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"shogun2sync/internal/applog"
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
			cmd := exec.Command(path, url)
			if err := cmd.Start(); err != nil {
				return err
			}
			// Start does not reap the process. Firefox normally hands the URL to
			// an existing browser and exits immediately, so wait off-thread to
			// avoid accumulating zombies without delaying the OAuth flow.
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	runtime.BrowserOpenURL(ctx, url)
	return nil
}

// App struct
type App struct {
	ctx  context.Context
	opMu sync.Mutex
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

func googleMirrorLocation(rootFolderID, subfolder string) (remoteSubfolder, localDir string, remoteRootIsSyncFolder bool) {
	remoteRootIsSyncFolder = strings.TrimSpace(rootFolderID) != ""
	remoteSubfolder = subfolder
	if remoteRootIsSyncFolder {
		remoteSubfolder = ""
	}
	cfg := config.Config{
		CloudProvider: "googledrive",
		CloudRoot:     paths.DefaultCloudRoot("googledrive"),
		SyncSubfolder: subfolder,
	}
	return remoteSubfolder, orchestrate.SyncTarget(cfg), remoteRootIsSyncFolder
}

// AuthorizeGoogleDrive runs the OAuth loopback flow (see internal/gdrive),
// then writes the resulting token into an app-owned rclone remote and creates
// the sync subfolder inside it.
//
// rootFolderID is the folder ID from a link a friend shared with you.
// Leave it empty if you're the one whose Drive is being shared — the
// subfolder gets created in your own "My Drive" root instead, and a
// shareable link for it comes back in the result so you can send it on.
func (a *App) AuthorizeGoogleDrive(rootFolderID, subfolder, clientID, clientSecret string) GoogleDriveAuthResult {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	if goruntime.GOOS != "linux" {
		return GoogleDriveAuthResult{OK: false, Error: "On Windows, choose the local shared folder mirrored by Google Drive for desktop; browser authorization is only needed on Linux."}
	}
	rootFolderID = strings.TrimSpace(rootFolderID)
	subfolder = strings.TrimSpace(subfolder)
	localRoot := paths.DefaultCloudRoot("googledrive")
	setupCfg := config.Config{CloudProvider: "googledrive", CloudRoot: localRoot, SyncSubfolder: subfolder}
	if err := orchestrate.ValidateConfig(setupCfg); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: err.Error()}
	}
	if current, err := config.Load(); err == nil {
		status := orchestrate.Status(current)
		if status.LinkedOK && filepath.Clean(status.SyncTarget) != filepath.Clean(orchestrate.SyncTarget(setupCfg)) {
			return GoogleDriveAuthResult{OK: false, Error: "Your save folder is still linked to a different sync folder. Use Troubleshooting → Stop syncing first, then start the new Google Drive setup."}
		}
	}
	if !rcloneutil.Installed() {
		return GoogleDriveAuthResult{OK: false, Error: "rclone is not installed"}
	}
	// Fail here rather than after the player has gone through a browser
	// login, only to have the background sync refuse to start.
	if err := bisync.CheckVersion(a.ctx); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: err.Error()}
	}

	creds := gdrive.Credentials{ClientID: strings.TrimSpace(clientID), ClientSecret: strings.TrimSpace(clientSecret)}
	if creds.ClientID == "" && creds.ClientSecret == "" {
		var err error
		creds, err = gdrive.Shared()
		if err != nil {
			return GoogleDriveAuthResult{OK: false, Error: err.Error()}
		}
	} else if err := creds.Validate(); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: err.Error()}
	}

	result, err := gdrive.Authorize(a.ctx, creds, func(url string) error {
		return openForOAuth(a.ctx, url)
	})
	if err != nil {
		applog.Printf("gdrive authorize failed: %v", err)
		return GoogleDriveAuthResult{OK: false, Error: err.Error()}
	}

	remoteName, err := rcloneutil.ConfigureGoogleDriveRemote(a.ctx, rcloneutil.PreferredGoogleDriveRemote,
		rootFolderID, result.TokenJSON, creds.ClientID, creds.ClientSecret)
	if err != nil {
		return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("saving rclone config: %v", err)}
	}
	if err := rcloneutil.VerifyAccess(a.ctx, remoteName); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("could not access the shared folder: %v", err)}
	}
	isGuest := rootFolderID != ""
	if !isGuest {
		if err := rcloneutil.EnsureSubfolder(a.ctx, remoteName, subfolder); err != nil {
			return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("creating sync folder: %v", err)}
		}
	}

	// The game always links to localRoot/subfolder. A host mirrors that exact
	// directory to remote:subfolder. A guest's remote is already scoped to the
	// shared folder, so it mirrors to remote: without appending the folder name
	// a second time. This invariant keeps both players on the same directory.
	remoteSubfolder, localDir, _ := googleMirrorLocation(rootFolderID, subfolder)
	if err := bisync.EnsureMirror(a.ctx, remoteName, remoteSubfolder, localDir, isGuest); err != nil {
		return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("setting up local sync mirror: %v", err)}
	}

	res := GoogleDriveAuthResult{OK: true}
	if !isGuest {
		link, err := rcloneutil.ShareableLink(a.ctx, remoteName, subfolder)
		if err != nil {
			return GoogleDriveAuthResult{OK: false, Error: fmt.Sprintf("creating a share link: %v", err)}
		}
		if strings.TrimSpace(link) == "" {
			return GoogleDriveAuthResult{OK: false, Error: "Google Drive did not return a share link; open Drive, share the sync folder manually, then try again"}
		}
		res.ShareLink = link
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
	// LastError describes the last failed run, empty when the last run was
	// fine. A sync that quietly stopped working is worse than one that
	// never started, so this gets shown rather than logged.
	LastError string `json:"lastError,omitempty"`
}

func (a *App) GetGoogleDriveMirrorStatus() GoogleDriveMirrorStatus {
	if !bisync.Available() {
		return GoogleDriveMirrorStatus{Applicable: false}
	}
	enabled, active := bisync.TimerStatus(a.ctx)
	last := bisync.LastSyncTime(a.ctx)
	res := GoogleDriveMirrorStatus{
		Applicable: true, Enabled: enabled, Active: active,
		LastError: bisync.LastError(a.ctx),
	}
	if !last.IsZero() {
		res.LastSync = last.Format("2006-01-02 15:04:05")
	}
	return res
}

// ---- Config ----

func (a *App) ConfigExists() bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	if cfg.SetupComplete {
		return true
	}
	// Migrate successful pre-1.0 configs without treating a config written by
	// an old failed/cancelled wizard as a completed setup.
	return orchestrate.Status(cfg).LinkedOK
}

func (a *App) GetConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}
	}
	return cfg
}

// Platform lets the wizard choose the native Drive Desktop folder flow on
// Windows and the rclone/bisync authorization flow on Linux.
func (a *App) Platform() string { return goruntime.GOOS }

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
	a.opMu.Lock()
	defer a.opMu.Unlock()
	res := orchestrate.Setup(cfg, savePathOverride)
	// Record where the save folder actually turned out to be. Once it's a
	// symlink into a non-default Steam library, auto-detection can't
	// rediscover it, so without this the Status view would call a working
	// setup broken from the next launch onwards.
	if res.OK {
		cfg.SavePath = res.SavePath
		cfg.SetupComplete = true
		if err := config.Save(cfg); err != nil {
			res.OK = false
			res.Error = fmt.Sprintf("Setup finished safely, but its settings could not be saved: %v. Press Finish again to retry; your saves are already linked.", err)
		} else if cfg.CloudProvider == "googledrive" && bisync.Available() {
			if err := bisync.EnableMirror(a.ctx); err != nil {
				res.OK = false
				res.Error = fmt.Sprintf("Your saves are linked safely, but Google Drive background sync could not start: %v. Press Finish again to retry.", err)
			}
		}
	}
	applog.Printf("setup: ok=%v alreadySet=%v savePath=%q target=%q err=%q",
		res.OK, res.AlreadySet, res.SavePath, res.SyncTarget, res.Error)
	return res
}

// RunUndo removes the link and restores a normal save folder. See
// orchestrate.Undo — the cloud folder is deliberately left untouched.
func (a *App) RunUndo() orchestrate.UndoResult {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return orchestrate.UndoResult{Error: fmt.Sprintf("loading settings: %v", err)}
	}
	if cfg.CloudProvider == "googledrive" {
		if err := bisync.DisableMirror(a.ctx); err != nil {
			return orchestrate.UndoResult{Error: err.Error(), SavePath: cfg.SavePath}
		}
	}
	res := orchestrate.Undo(cfg)
	if res.OK {
		cfg.SetupComplete = false
		if err := config.Save(cfg); err != nil {
			res.OK = false
			res.Error = fmt.Sprintf("Syncing stopped and your local saves were restored, but the app could not save that status: %v", err)
		}
	}
	applog.Printf("undo: ok=%v restored=%d err=%q", res.OK, res.Restored, res.Error)
	return res
}

// GetLogTail returns the end of the log file so a player can copy it into a
// bug report without going looking for it on disk.
func (a *App) GetLogTail() string {
	s, err := applog.Tail(64 << 10)
	if err != nil {
		return fmt.Sprintf("Couldn't read the log: %v", err)
	}
	return s
}

func (a *App) GetStatus() orchestrate.StatusResult {
	cfg, err := config.Load()
	if err != nil {
		return orchestrate.StatusResult{ConfigExists: config.Exists(), Error: fmt.Sprintf("loading settings: %v", err)}
	}
	return orchestrate.Status(cfg)
}

func (a *App) RunRecover() orchestrate.RecoverResult {
	cfg, err := config.Load()
	if err != nil {
		return orchestrate.RecoverResult{OK: false, Error: fmt.Sprintf("loading settings: %v", err)}
	}
	return orchestrate.Recover(cfg)
}

// ResolveConflict moves a conflict file into a recoverable .trash folder.
// Returns "" on success or an error message.
func (a *App) ResolveConflict(path string) string {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Sprintf("loading settings: %v", err)
	}
	if err := orchestrate.ResolveConflict(cfg, path); err != nil {
		return err.Error()
	}
	return ""
}

// PromoteConflict keeps the selected duplicate as the canonical save while
// preserving the previous original in recovery trash.
func (a *App) PromoteConflict(path string) string {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Sprintf("loading settings: %v", err)
	}
	if err := orchestrate.PromoteConflict(cfg, path); err != nil {
		return err.Error()
	}
	return ""
}

// OpenExternal opens a web link in the player's browser. Links inside the
// app's own webview would otherwise navigate away from the UI with no way
// back — there's no address bar to return from.
func (a *App) OpenExternal(url string) {
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		runtime.BrowserOpenURL(a.ctx, url)
	}
}

// OpenInFileManager opens the folder containing path in the OS's native
// file browser, for players who'd rather look at the folder themselves.
// Passing a file is fine — the folder holding it is what opens, which
// spares the caller from having to split the path (and getting the
// separator wrong on one of the two platforms).
func (a *App) OpenInFileManager(path string) {
	if path == "" {
		return
	}
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	runtime.BrowserOpenURL(a.ctx, paths.FileURL(dir))
}
