package bisync

import "os"

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o755)
}

func mkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func removeAll(path string) error { return os.RemoveAll(path) }
