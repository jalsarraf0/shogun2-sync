//go:build !windows

package linkutil

// junctionTarget is a Windows-only concept; elsewhere links are always
// plain symlinks, which Inspect already handles via os.Readlink.
func junctionTarget(path string) (target string, ok bool) {
	return "", false
}
