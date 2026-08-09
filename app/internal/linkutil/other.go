//go:build !windows

package linkutil

import "fmt"

// junctionTarget is a Windows-only concept; elsewhere links are always
// plain symlinks, which Inspect already handles via os.Readlink.
func junctionTarget(path string) (target string, ok bool) {
	return "", false
}

// createJunction never runs off Windows — Link only reaches for it there.
func createJunction(link, target string) error {
	return fmt.Errorf("junctions are Windows-only")
}
