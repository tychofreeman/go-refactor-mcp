package edit

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FilesUnder returns path when it is a file, or the files under it when it is
// a directory.
func FilesUnder(path string) ([]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return []string{path}, nil
	}
	var out []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			out = append(out, p)
		}
		return err
	})
	return out, err
}
