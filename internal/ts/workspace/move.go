package workspace

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/edit"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
)

// Under returns path when it is a file, or the files under it when it is
// a directory.
func Under(path string) ([]string, error) {
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

// MoveFile returns the edits that move the file or directory from to to:
// each file written at its new path with the changes the language
// service makes to it (its own relative imports), the old one deleted,
// and the files that refer to them changed to refer to the new paths.
func (w *Workspace) MoveFile(from, to string) ([]edit.FileEdit, error) {
	paths, err := Under(from)
	if err != nil {
		return nil, err
	}
	byFile := map[string][]edit.TextEdit{}
	seen := map[tsapi.Edit]bool{}
	for _, p := range w.projects {
		for _, e := range p.FileRename(context.Background(), from, to) {
			if seen[e] || !w.Contains(e.File) {
				continue
			}
			seen[e] = true
			byFile[filepath.FromSlash(e.File)] = append(byFile[filepath.FromSlash(e.File)], edit.TextEdit{Start: e.Start, End: e.End, NewText: e.NewText})
		}
	}
	var out []edit.FileEdit
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if es := byFile[p]; len(es) > 0 {
			if data, err = edit.Apply(data, es); err != nil {
				return nil, err
			}
		}
		delete(byFile, p)
		dst := filepath.Join(to, strings.TrimPrefix(p, from))
		out = append(out, edit.FileEdit{Path: dst, Edits: []edit.TextEdit{{NewText: string(data)}}, Verbatim: true}, edit.FileEdit{Path: p, Delete: true})
	}
	var rest []string
	for f := range byFile {
		rest = append(rest, f)
	}
	slices.Sort(rest)
	for _, f := range rest {
		out = append(out, edit.FileEdit{Path: f, Edits: byFile[f]})
	}
	return out, nil
}

// Importers returns where files outside path (a file, or a directory with
// what is under it) refer to a file in it, each once, relative to the
// root and in order: "src/a.ts:3".
func (w *Workspace) Importers(path string) []string {
	var out []string
	for _, p := range w.projects {
		for _, at := range p.Importers(context.Background(), path) {
			if at = w.Rel(at); !slices.Contains(out, at) {
				out = append(out, at)
			}
		}
	}
	slices.Sort(out)
	return out
}
