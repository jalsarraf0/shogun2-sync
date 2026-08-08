package linkutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectOnMissingPath(t *testing.T) {
	dir := t.TempDir()
	st, err := Inspect(filepath.Join(dir, "nope"), filepath.Join(dir, "target"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Exists || st.IsLink {
		t.Fatalf("expected empty status for missing path, got %+v", st)
	}
}

func TestLinkAndInspectMatches(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "save")

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Link(link, target); err != nil {
		t.Fatal(err)
	}

	st, err := Inspect(link, target)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsLink || !st.MatchesTarget {
		t.Fatalf("expected link matching target, got %+v", st)
	}
}

func TestInspectDetectsWrongTarget(t *testing.T) {
	dir := t.TempDir()
	realTarget := filepath.Join(dir, "real")
	otherTarget := filepath.Join(dir, "other")
	link := filepath.Join(dir, "save")

	os.MkdirAll(realTarget, 0o755)
	os.MkdirAll(otherTarget, 0o755)
	if err := Link(link, realTarget); err != nil {
		t.Fatal(err)
	}

	st, err := Inspect(link, otherTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsLink || st.MatchesTarget {
		t.Fatalf("expected link NOT matching a different target, got %+v", st)
	}
}

// copyPath is what MoveContents falls back to when the save folder and the
// cloud folder are on different drives and os.Rename can't cross between
// them. Exercised directly, since a test can't conjure a second filesystem.
func TestCopyPathRecursesAndPreservesModTimes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(src, "sub", "turn090.save")
	if err := os.WriteFile(inner, []byte("save data"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(inner, old, old); err != nil {
		t.Fatal(err)
	}

	if err := copyPath(src, dst); err != nil {
		t.Fatal(err)
	}

	copied := filepath.Join(dst, "sub", "turn090.save")
	data, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "save data" {
		t.Fatalf("contents = %q, want %q", data, "save data")
	}

	info, err := os.Stat(copied)
	if err != nil {
		t.Fatal(err)
	}
	// Recover ranks saves by mtime, so stamping copies with "now" would
	// scramble exactly the ordering a desyncing player needs.
	if !info.ModTime().Truncate(time.Second).Equal(old) {
		t.Errorf("mtime = %v, want %v", info.ModTime(), old)
	}
}

func TestMoveContentsHandlesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "a.save"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MoveContents(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "nested", "a.save")); err != nil {
		t.Fatalf("expected nested file moved: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected src removed, stat err = %v", err)
	}
}

func TestMoveContentsMovesEverythingAndRemovesSrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	os.WriteFile(filepath.Join(src, "a.save"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(src, "b.save"), []byte("b"), 0o644)

	if err := MoveContents(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected src to be removed, stat err = %v", err)
	}
	for _, name := range []string{"a.save", "b.save"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Fatalf("expected %s in dst: %v", name, err)
		}
	}
}
