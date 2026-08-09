package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	cases := map[string]string{
		"~":            home,
		"~/Dropbox":    filepath.Join(home, "Dropbox"),
		"/absolute":    "/absolute",
		"relative/dir": "relative/dir",
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultCloudRootKnownProviders(t *testing.T) {
	for _, p := range []string{"dropbox", "onedrive", "googledrive"} {
		if got := DefaultCloudRoot(p); got == "" && !(p == "googledrive" && runtime.GOOS != "linux") {
			t.Errorf("DefaultCloudRoot(%q) returned empty string", p)
		}
	}
}

// A game installed to a second-drive Steam library keeps its Proton prefix
// there too, so the save folder is only findable via libraryfolders.vdf.
func TestSteamLibraryRootsReadsLibraryFoldersVDF(t *testing.T) {
	home := t.TempDir()
	extra := filepath.Join(t.TempDir(), "SteamLibrary")

	steamapps := filepath.Join(home, ".steam", "steam", "steamapps")
	if err := os.MkdirAll(steamapps, 0o755); err != nil {
		t.Fatal(err)
	}
	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"` + filepath.Join(home, ".steam", "steam") + `"
		"label"		""
	}
	"1"
	{
		"path"		"` + extra + `"
		"label"		""
	}
}
`
	if err := os.WriteFile(filepath.Join(steamapps, "libraryfolders.vdf"), []byte(vdf), 0o644); err != nil {
		t.Fatal(err)
	}

	roots := steamLibraryRoots(home)
	var found bool
	for _, r := range roots {
		if r == extra {
			found = true
		}
	}
	if !found {
		t.Fatalf("extra library %q missing from roots %v", extra, roots)
	}

	// The same path listed twice (once as an install root, once in the
	// VDF) must not produce duplicate candidates.
	seen := map[string]int{}
	for _, r := range roots {
		seen[r]++
	}
	for r, n := range seen {
		if n > 1 {
			t.Errorf("root %q appears %d times", r, n)
		}
	}
}

func TestDetectSavePathFindsExtraSteamLibrary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Steam library layout is Linux-only in this app")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	extra := filepath.Join(t.TempDir(), "SteamLibrary")
	save := filepath.Join(extra, "steamapps", "compatdata", shogun2AppID,
		"pfx", "drive_c", "users", "steamuser", "AppData", "Roaming",
		"The Creative Assembly", "Shogun2", "save_games_multiplayer")
	if err := os.MkdirAll(save, 0o755); err != nil {
		t.Fatal(err)
	}

	steamapps := filepath.Join(home, ".steam", "steam", "steamapps")
	if err := os.MkdirAll(steamapps, 0o755); err != nil {
		t.Fatal(err)
	}
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"" + extra + "\"\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(steamapps, "libraryfolders.vdf"), []byte(vdf), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := DetectSavePath(); got != save {
		t.Fatalf("DetectSavePath() = %q, want %q", got, save)
	}
}

func TestFileURL(t *testing.T) {
	// "The Creative Assembly" guarantees a space in every real save path,
	// so escaping is not a corner case here.
	got := FileURL(filepath.Join(string(filepath.Separator), "home", "p", "The Creative Assembly"))
	if !strings.HasPrefix(got, "file:///") {
		t.Errorf("FileURL = %q, want a file:/// prefix", got)
	}
	if strings.Contains(got, " ") {
		t.Errorf("FileURL = %q, spaces should be percent-escaped", got)
	}
}
