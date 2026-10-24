// Command views prints a module for the call-tree probe
// (docs/reading-and-deriving.md, step 2): skeleton, each Go file as
// inspect shows it, without function bodies; or whole, each Go file as
// it is. Test files are left out. The third view is cmd/calltree.
//
//	views skeleton|whole DIR
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/inspect"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: views skeleton|whole DIR")
		os.Exit(2)
	}
	out, err := view(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "views:", err)
		os.Exit(1)
	}
	fmt.Print(out)
}

func view(kind string, dir string) (string, error) {
	files, err := goFiles(dir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	switch kind {
	case "whole":
		for _, f := range files {
			src, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "== %s\n%s\n", f, src)
		}
	case "skeleton":
		ws, err := workspace.Load(dir)
		if err != nil {
			return "", err
		}
		for _, f := range files {
			s, err := inspect.File(ws, inspect.Query{File: f}, inspect.Options{})
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "== %s\n%s\n", f, s)
		}
	default:
		return "", fmt.Errorf("no view %q: skeleton or whole", kind)
	}
	return b.String(), nil
}

// goFiles lists the Go files under dir that are not tests, relative to
// dir, outside testdata.
func goFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	return out, err
}
