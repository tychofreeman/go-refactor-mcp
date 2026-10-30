package lang

import (
	"os"
	"path/filepath"
)

// Exists reports whether a file or directory is at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ExistingDir returns dir, or the nearest directory above it that exists.
func ExistingDir(dir string) string {
	for !Exists(dir) && filepath.Dir(dir) != dir {
		dir = filepath.Dir(dir)
	}
	return dir
}

// RelTo returns path relative to base, with forward slashes, or path
// itself when it has none.
func RelTo(base, path string) string {
	if r, err := filepath.Rel(base, path); err == nil {
		return filepath.ToSlash(r)
	}
	return path
}
