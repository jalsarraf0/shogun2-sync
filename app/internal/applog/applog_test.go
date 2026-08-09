package applog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRotateReplacesPreviousGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("n"), maxSize), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	rotate(path)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("current log should have moved, stat err=%v", err)
	}
	got, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxSize {
		t.Fatalf("rotated log size=%d, want %d; stale generation may have survived", len(got), maxSize)
	}
}
