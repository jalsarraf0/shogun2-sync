// Package paths knows where Shogun 2 keeps its multiplayer saves and where
// each cloud provider's desktop client keeps its synced folder, on both
// Linux (Steam/Proton) and Windows (native).
package paths

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// shogun2AppID is Shogun 2's Steam application ID, which names its Proton
// prefix directory.
const shogun2AppID = "34330"

// steamInstallRoots are the places Steam itself can be installed on Linux,
// depending on how it was packaged (native package, ~/.local/share, or
// Flatpak).
func steamInstallRoots(home string) []string {
	return []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "root"),
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".steam", "steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
	}
}

// libraryPathRe pulls the "path" values out of steamapps/libraryfolders.vdf.
var libraryPathRe = regexp.MustCompile(`(?m)^\s*"path"\s+"(.*)"\s*$`)

// steamLibraryRoots returns every Steam library directory on this machine:
// the roots Steam installs itself into, plus any extra library folders the
// user added elsewhere. Reading libraryfolders.vdf matters because a large
// Steam library usually lives on a second drive, and a game installed there
// keeps its Proton prefix — and therefore its save folder — on that drive
// too, where none of the default paths would ever find it.
func steamLibraryRoots(home string) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, root := range steamInstallRoots(home) {
		add(root)
		data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, m := range libraryPathRe.FindAllStringSubmatch(string(data), -1) {
			// VDF escapes backslashes; harmless to undo on Linux paths.
			add(strings.ReplaceAll(m[1], `\\`, `\`))
		}
	}
	return out
}

// steamCompatDataCandidates are the places a Proton prefix for Shogun 2 can
// live on Linux, one per known Steam library.
func steamCompatDataCandidates(home string) []string {
	var out []string
	for _, root := range steamLibraryRoots(home) {
		out = append(out, filepath.Join(root, "steamapps", "compatdata", shogun2AppID,
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
		if runtime.GOOS != "linux" {
			// Drive for desktop can use a drive letter, a mirrored directory, or
			// a user-chosen mount. Guessing ~/My Drive is usually wrong and lets a
			// setup appear successful without touching the shared folder. Require
			// the player to choose the local mirrored folder explicitly.
			return ""
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

// FileURL converts a local filesystem path into a file:// URL suitable for
// handing to the OS's default handler.
//
// Concatenating "file://" onto a path is wrong on Windows: a drive-letter
// path needs a third slash and forward separators
// ("file:///C:/Users/..."), otherwise the drive letter is parsed as the
// URL's host and nothing opens. Spaces and other reserved characters need
// escaping on every platform — and the Shogun 2 save path always contains
// one, in "The Creative Assembly".
func FileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if runtime.GOOS == "windows" {
		abs = filepath.ToSlash(abs)
		if !strings.HasPrefix(abs, "/") {
			abs = "/" + abs
		}
	}
	u := url.URL{Scheme: "file", Path: abs}
	return u.String()
}
