package paths

import (
	"os"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	cases := map[string]string{
		"~":            home,
		"~/Dropbox":    home + "/Dropbox",
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
		if got := DefaultCloudRoot(p); got == "" {
			t.Errorf("DefaultCloudRoot(%q) returned empty string", p)
		}
	}
}
