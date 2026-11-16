package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Outside returns an error when path, with the links on it resolved, is
// not root or inside it, root's links resolved too: a link inside the
// workspace may not carry a change out of it. A path that does not exist
// yet is judged by the nearest directory above it that does.
func Outside(root, path string) error {
	realRoot, err := resolve(root)
	if err != nil {
		return err
	}
	real, err := resolve(path)
	if err != nil {
		return err
	}
	r, err := filepath.Rel(realRoot, real)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return fmt.Errorf("it is reached through a link and is %s, outside %s", real, root)
	}
	return nil
}

// resolve returns path with every link on it resolved. Of a path that
// does not exist, the existing directory above it is resolved and the
// rest joined to it; a link that points to nothing is an error.
func resolve(path string) (string, error) {
	rest := ""
	for p := path; ; {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if fi, lerr := os.Lstat(p); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("it is reached through %s, a link that points to nothing", p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path, nil
		}
		rest, p = filepath.Join(filepath.Base(p), rest), parent
	}
}
