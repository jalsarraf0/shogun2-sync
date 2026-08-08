package linkutil

import (
	"os"
	"path/filepath"
	"testing"
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
