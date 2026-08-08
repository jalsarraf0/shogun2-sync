// Package paths knows where Shogun 2 keeps its multiplayer saves and where
// each cloud provider's desktop client keeps its synced folder, on both
// Linux (Steam/Proton) and Windows (native).
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// steamCompatDataCandidates are the places a Proton prefix for Shogun 2
// (Steam appid 34330) can live on Linux, depending on how Steam itself was
// installed (native package, ~/.local/share, or Flatpak).
func steamCompatDataCandidates(home string) []string {
	const appID = "34330"
	roots := []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".steam", "steam"),
	}
	var out []string
	for _, root := range roots {
		out = append(out, filepath.Join(root, "steamapps", "compatdata", appID,
			"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming",
			"The Creative Assembly", "Shogun2", "save_games_multiplayer"))
	}
	return out
}

// DetectSavePath looks for an existing Shogun 2 multiplayer save folder.
// Returns "" if none of the known locations exist yet (e.g. the game has
// never been run, so the folder hasn't been created).
func DetectSavePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	var candidates []string
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			candidates = append(candidates, filepath.Join(appData, "The Creative Assembly", "Shogun2", "save_games_multiplayer"))
		}
	} else {
		candidates = steamCompatDataCandidates(home)
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}

// ExpectedSavePath returns where the save folder *should* end up once the
// game has been run at least once, even if it doesn't exist yet - used to
// tell the user where to look / what we'll be linking.
func ExpectedSavePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return ""
		}
		return filepath.Join(appData, "The Creative Assembly", "Shogun2", "save_games_multiplayer")
	}
	candidates := steamCompatDataCandidates(home)
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// DefaultCloudRoot returns the usual local sync folder for a given
// provider ("dropbox", "onedrive", "googledrive"), before the user
// confirms/overrides it.
func DefaultCloudRoot(provider string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch provider {
	case "dropbox":
		return filepath.Join(home, "Dropbox")
	case "onedrive":
		if runtime.GOOS == "windows" {
			if v := os.Getenv("OneDrive"); v != "" {
				return v
			}
		}
		return filepath.Join(home, "OneDrive")
	case "googledrive":
		if runtime.GOOS == "windows" {
			return filepath.Join(home, "My Drive")
		}
		// Linux: our own rclone-bisync mirror directory, not a native client.
		return filepath.Join(home, "GoogleDriveSync")
	default:
		return home
	}
}

// ExpandHome expands a leading ~ to the user's home directory.
func ExpandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == filepath.Separator) {
		return filepath.Join(home, p[2:])
	}
	return p
}

// Exists reports whether a directory exists at p.
func Exists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
