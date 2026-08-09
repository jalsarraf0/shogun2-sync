//go:build windows

package linkutil

import (
	"os"
	"path/filepath"
	"testing"
)

// Link must succeed on a stock Windows account. Directory junctions need no
// special rights, unlike symlinks, which require Administrator or Developer
// Mode — so if this ever starts falling through to the symlink branch, it
// will fail here rather than in a player's hands.
func TestLinkCreatesJunctionWithoutElevation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "save_games_multiplayer")

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "turn090.save"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Link(link, target); err != nil {
		t.Fatalf("Link: %v", err)
	}

	// junctionTarget is the reparse-point reader Inspect relies on; Go's
	// stdlib doesn't report junctions as symlinks, so this is the only
	// thing standing between us and "your save folder isn't linked".
	got, ok := junctionTarget(link)
	if !ok {
		t.Fatal("junctionTarget did not recognise the link as a junction")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("junction target %q is not absolute", got)
	}

	st, err := Inspect(link, target)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsLink || !st.MatchesTarget {
		t.Fatalf("Inspect = %+v, want a link matching the target", st)
	}

	// The link has to actually behave like the folder the game writes to.
	if _, err := os.Stat(filepath.Join(link, "turn090.save")); err != nil {
		t.Errorf("reading through the junction failed: %v", err)
	}
}

// Every real Shogun 2 save path contains "The Creative Assembly", so a
// space in the path is guaranteed, not an edge case.
func TestLinkHandlesSpacesInPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "The Creative Assembly", "Shogun2", "save_games_multiplayer")
	link := filepath.Join(dir, "My Cloud Folder", "Shogun2SaveSync")

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Link(link, target); err != nil {
		t.Fatalf("Link with spaces in the path: %v", err)
	}

	st, err := Inspect(link, target)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsLink || !st.MatchesTarget {
		t.Fatalf("Inspect = %+v, want a link matching the target", st)
	}
}

// A real directory must not be mistaken for a junction, or Setup would skip
// moving the player's existing saves into the cloud folder.
func TestJunctionTargetIgnoresPlainDirectory(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if target, ok := junctionTarget(plain); ok {
		t.Fatalf("plain directory reported as junction pointing at %q", target)
	}
}
