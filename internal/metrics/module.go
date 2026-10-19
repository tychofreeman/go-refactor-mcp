package metrics

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReadModule reads the Go files of the module at root, leaving out
// testdata, vendor and hidden directories; file paths are relative to
// root.
func ReadModule(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (strings.HasPrefix(n, ".") || n == "vendor" || n == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
