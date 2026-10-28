package workspace

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
	"golang.org/x/tools/go/packages"
)

// Fresh loads without go list.
//
// A full load runs `go list`, which costs most of a load for a small
// module, to learn the package metadata: which packages there are, their
// files and what they import. Tests compare an incrementally updated
// workspace with a fresh load, after every change; when the change kept
// the metadata, the fresh load can take it from an earlier load and only
// parse and type-check, in process. Fingerprint tells when it was kept.

// Metadata is the package metadata of a load, without syntax or types.
type Metadata struct {
	root, base string
	patterns   []string
	// fset holds the positions of the loaded packages' imports, so the
	// files are parsed into it too.
	fset *token.FileSet
	// pkgs are the module's packages, as sourcePackages returns them: the
	// first roots are the loaded packages, the rest test variants of their
	// dependencies. Their Imports point at each other, or at the loaded
	// packages of the standard library and other modules, which no edit
	// changes.
	pkgs  []*packages.Package
	roots int
}

// Metadata returns w's package metadata, or false when its packages
// cannot be type-checked from their files alone (cgo, loader errors).
func (w *Workspace) Metadata() (*Metadata, bool) {
	src := w.sourcePackages()
	for _, p := range src {
		if !canRecheck(p) {
			return nil, false
		}
	}
	return &Metadata{
		root: w.Root, base: w.Base, patterns: w.Patterns, fset: w.Fset,
		pkgs: metadataOf(src), roots: len(w.Pkgs),
	}, true
}

// Load parses and type-checks the files on disk as Load would, taking the
// package metadata from m instead of go list.
func (m *Metadata) Load() (*Workspace, error) {
	stamps, err := snapshot(m.root)
	if err != nil {
		return nil, err
	}
	w := &Workspace{Root: m.root, Base: m.base, Patterns: m.patterns, Fset: m.fset, stamps: stamps, History: &history.Stack{}}
	pkgs := metadataOf(m.pkgs)
	w.Pkgs = pkgs[:m.roots]
	if err := check(w.Fset, pkgs, m.roots); err != nil {
		return nil, err
	}
	return w, nil
}

// check parses the files of pkgs into fset, each once, and type-checks
// pkgs in dependency order: the first roots of them as roots, the rest,
// test variants of their dependencies, without function bodies. A
// package's errors are the loader's, then its parse and type errors.
func check(fset *token.FileSet, pkgs []*packages.Package, roots int) error {
	// Parse each file once: the packages that contain it share its syntax,
	// as they do after a full load.
	syntax := map[string]*ast.File{}
	parseErrs := map[string][]packages.Error{}
	for _, p := range pkgs {
		for _, path := range p.CompiledGoFiles {
			if _, ok := syntax[path]; ok {
				continue
			}
			f, err := parser.ParseFile(fset, path, nil, parser.AllErrors|parser.ParseComments)
			if f == nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}
			syntax[path] = f
			if err != nil {
				parseErrs[path] = parseErrorsOf(err)
			}
		}
	}

	set := map[*packages.Package]bool{}
	for _, p := range pkgs {
		set[p] = true
	}
	w := &Workspace{Fset: fset}
	for _, p := range dependencyOrder(set) {
		var files []*ast.File
		var errs []packages.Error
		for _, e := range p.Errors {
			if e.Kind != packages.ParseError && e.Kind != packages.TypeError {
				errs = append(errs, e)
			}
		}
		for _, path := range p.CompiledGoFiles {
			files = append(files, syntax[path])
			errs = append(errs, parseErrs[path]...)
		}
		w.typeCheck(p, slices.Index(pkgs, p) >= roots, files, errs)
	}
	return nil
}

// metadataOf copies the metadata of the packages src, leaving out syntax,
// types and errors. The copies import each other where the originals did.
func metadataOf(src []*packages.Package) []*packages.Package {
	copies := map[*packages.Package]*packages.Package{}
	out := make([]*packages.Package, len(src))
	for i, p := range src {
		out[i] = &packages.Package{
			ID:              p.ID,
			Name:            p.Name,
			PkgPath:         p.PkgPath,
			GoFiles:         p.GoFiles,
			CompiledGoFiles: p.CompiledGoFiles,
			OtherFiles:      p.OtherFiles,
			IgnoredFiles:    p.IgnoredFiles,
			ExportFile:      p.ExportFile,
			Module:          p.Module,
			TypesSizes:      p.TypesSizes,
		}
		copies[p] = out[i]
	}
	for _, p := range src {
		c := copies[p]
		c.Imports = map[string]*packages.Package{}
		for path, imp := range p.Imports {
			if ci, ok := copies[imp]; ok {
				imp = ci
			}
			c.Imports[path] = imp
		}
	}
	return out
}

// Fingerprint summarizes what go list's output for the module at root
// depends on: its Go files, each one's text up to the end of its imports,
// and go.mod, go.sum and go.work. Loads of files with the same fingerprint
// have the same metadata.
func Fingerprint(root string) (string, error) {
	h := sha256.New()
	err := walkLoadFiles(root, func(p string, d fs.DirEntry) error {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".go") {
			src = header(src)
		}
		fmt.Fprintf(h, "%s\x00%d\x00%s", history.Rel(root, p), len(src), src)
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// header returns a Go file's text up to the end of its imports: what go
// list reads of it. If the file does not parse that far, it is all of it.
func header(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return src
	}
	end := f.Name.End()
	if len(f.Decls) > 0 {
		end = f.Decls[len(f.Decls)-1].End()
	}
	return src[:fset.Position(end).Offset]
}
