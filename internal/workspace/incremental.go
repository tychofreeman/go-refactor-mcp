package workspace

// Incremental updates.
//
// A full reload runs `go list` and type-checks every package from scratch:
// about 0.1-0.2 s even for a small module, and much more for a large one.
// After an edit to existing files, most of that work is unnecessary: the
// package metadata (which files belong to which package, what they import)
// is unchanged, the standard library and dependencies are unchanged, and
// only packages that contain or depend on the edited files can have
// different type information.
//
// update therefore re-parses only the edited files and re-type-checks, in
// process and in dependency order, only the packages containing them plus
// every package in the workspace that imports one of those (directly or
// indirectly). It configures the type checker exactly as go/packages does,
// so the result matches a full reload; tests compare the two (Compare).
//
// Whenever the metadata could have changed, it falls back to a full
// reload: a file's package name, imports or build constraints changed, a
// file appeared that the loaded packages do not contain but the build
// would, a package uses cgo, or a package had loader errors.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// errNeedReload means the edit may have changed package metadata, so only
// a full reload is correct.
var errNeedReload = errors.New("package metadata may have changed")

// update brings the workspace up to date after the given files (absolute
// paths) were modified, incrementally when possible.
func (w *Workspace) update(changed []string) error {
	w.effects = nil
	err := w.updateIncrementally(changed)
	if errors.Is(err, errNeedReload) {
		return w.Reload()
	}
	if err != nil {
		return err
	}
	for _, p := range changed {
		if fi, err := os.Stat(p); err == nil {
			w.stamps[p] = stamp{fi.Size(), fi.ModTime().UnixNano()}
		}
	}
	w.IncrementalUpdates++
	return nil
}

func (w *Workspace) updateIncrementally(changed []string) error {
	pkgs := w.sourcePackages()

	// Which packages contain each file, and its current syntax tree.
	owners := map[string][]*packages.Package{}
	oldSyntax := map[string]*ast.File{}
	for _, p := range pkgs {
		for _, f := range p.CompiledGoFiles {
			owners[f] = append(owners[f], p)
		}
		for _, f := range p.Syntax {
			tf := w.Fset.File(f.Pos())
			if tf == nil {
				return errNeedReload // a file that did not parse as far as its package clause
			}
			oldSyntax[tf.Name()] = f
		}
	}

	// Parse the changed files once; the packages that contain a file
	// (e.g. "p" and its test variant "p [p.test]") share its syntax tree,
	// as they do after a full load.
	newSyntax := map[string]*ast.File{}
	parseErrs := map[string]error{}
	var direct []*packages.Package
	for _, path := range changed {
		if !strings.HasSuffix(path, ".go") {
			return errNeedReload
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return errNeedReload // deleted or unreadable
		}
		if len(owners[path]) == 0 {
			// Not in any loaded package. That only matters if the build
			// would now include it (e.g. its build constraint changed).
			if ctxt := build.Default; matchFile(&ctxt, path) {
				return errNeedReload
			}
			continue
		}
		f, err := parser.ParseFile(w.Fset, path, src, parser.AllErrors|parser.ParseComments)
		if f == nil {
			return errNeedReload
		}
		old := oldSyntax[path]
		if old == nil || !sameHeader(old, f) {
			return errNeedReload
		}
		newSyntax[path] = f
		if err != nil {
			parseErrs[path] = err
		}
		direct = append(direct, owners[path]...)
	}
	if len(direct) == 0 {
		return nil
	}

	// Everything that can see a change: the packages containing the files
	// and, transitively, the workspace packages that import them.
	inWorkspace := map[*packages.Package]bool{}
	importers := map[*packages.Package][]*packages.Package{}
	for _, p := range pkgs {
		inWorkspace[p] = true
	}
	for _, p := range pkgs {
		for _, imp := range p.Imports {
			if inWorkspace[imp] {
				importers[imp] = append(importers[imp], p)
			}
		}
	}
	affected := map[*packages.Package]bool{}
	var mark func(p *packages.Package)
	mark = func(p *packages.Package) {
		if affected[p] {
			return
		}
		affected[p] = true
		for _, q := range importers[p] {
			mark(q)
		}
	}
	for _, p := range direct {
		mark(p)
	}

	// Check that every affected package can be re-checked here.
	for p := range affected {
		if !canRecheck(p) {
			return errNeedReload
		}
	}

	// Re-check in dependency order, so that each importer sees the new
	// types of its dependencies (Imports points at the same Package values
	// whose Types are replaced in place).
	order := dependencyOrder(affected)

	roots := map[*packages.Package]bool{}
	for _, p := range w.Pkgs {
		roots[p] = true
	}
	for _, p := range order {
		w.recheck(p, !roots[p], newSyntax, parseErrs)
	}
	return nil
}

// dependencyOrder returns the packages of set, each after those of set it
// imports, deterministically.
func dependencyOrder(set map[*packages.Package]bool) []*packages.Package {
	var order []*packages.Package
	visited := map[*packages.Package]bool{}
	var visit func(p *packages.Package)
	visit = func(p *packages.Package) {
		if visited[p] {
			return
		}
		visited[p] = true
		paths := make([]string, 0, len(p.Imports))
		for path := range p.Imports {
			paths = append(paths, path)
		}
		sort.Strings(paths) // deterministic order
		for _, path := range paths {
			if imp := p.Imports[path]; set[imp] {
				visit(imp)
			}
		}
		order = append(order, p)
	}
	sorted := make([]*packages.Package, 0, len(set))
	for p := range set {
		sorted = append(sorted, p)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, p := range sorted {
		visit(p)
	}
	return order
}

// sourcePackages returns the root packages (w.Pkgs) followed by every
// other package of the module that they import, directly or indirectly.
//
// The others are test variants of dependencies: when an external test
// package a_test imports a package h that imports a, go/packages builds
// "h [a.test]", compiled from source against "a [a.test]". It is not a root
// package, but an edit to a (or to h) must re-check it, or a_test would see
// two different versions of a's types.
func (w *Workspace) sourcePackages() []*packages.Package {
	out := append([]*packages.Package(nil), w.Pkgs...)
	seen := map[*packages.Package]bool{}
	for _, p := range w.Pkgs {
		seen[p] = true
	}
	var extra []*packages.Package
	var visit func(p *packages.Package)
	visit = func(p *packages.Package) {
		for _, imp := range p.Imports {
			if seen[imp] {
				continue
			}
			seen[imp] = true
			if len(imp.CompiledGoFiles) == 0 || !w.Contains(imp.CompiledGoFiles[0]) {
				continue // standard library or another module: never edited
			}
			extra = append(extra, imp)
			visit(imp)
		}
	}
	for _, p := range w.Pkgs {
		visit(p)
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].ID < extra[j].ID })
	return append(out, extra...)
}

// canRecheck reports whether p's metadata lets it be re-type-checked from
// its syntax: no cgo (its compiled files are generated) and no loader
// errors (they could hide metadata problems the type checker can't see).
func canRecheck(p *packages.Package) bool {
	if p.Types == nil || p.TypesInfo == nil || len(p.Syntax) != len(p.CompiledGoFiles) {
		return false
	}
	if len(p.GoFiles) != len(p.CompiledGoFiles) {
		return false // cgo
	}
	for i := range p.GoFiles {
		if p.GoFiles[i] != p.CompiledGoFiles[i] {
			return false
		}
	}
	for _, e := range p.Errors {
		if e.Kind != packages.ParseError && e.Kind != packages.TypeError {
			return false
		}
	}
	return true
}

// recheck re-type-checks p with the new syntax for changed files, replacing
// its Syntax, Types, TypesInfo and errors (see typeCheck).
func (w *Workspace) recheck(p *packages.Package, dep bool, newSyntax map[string]*ast.File, parseErrs map[string]error) {
	files := make([]*ast.File, len(p.Syntax))
	replaced := map[string]bool{}
	for i, f := range p.Syntax {
		name := w.Fset.File(f.Pos()).Name()
		if nf, ok := newSyntax[name]; ok {
			f = nf
			replaced[name] = true
		}
		files[i] = f
	}

	// Parse errors in files that were not re-parsed still apply.
	var errs []packages.Error
	for _, e := range p.Errors {
		if e.Kind == packages.ParseError && !replaced[errorFile(e.Pos)] {
			errs = append(errs, e)
		}
	}
	for _, f := range p.CompiledGoFiles {
		if err := parseErrs[f]; err != nil && replaced[f] {
			errs = append(errs, parseErrorsOf(err)...)
		}
	}

	w.typeCheck(p, dep, files, errs)
}

// typeCheck type-checks p from files, configured as go/packages does, and
// sets its Syntax, Types, TypesInfo and errors: errs (parse errors)
// followed by the type errors. Like go/packages, it skips function bodies
// in packages that are not roots (dep is true): only their declarations
// matter to the packages importing them.
func (w *Workspace) typeCheck(p *packages.Package, dep bool, files []*ast.File, errs []packages.Error) {
	tpkg := types.NewPackage(p.PkgPath, p.Name)
	info := &types.Info{
		Types:        map[ast.Expr]types.TypeAndValue{},
		Defs:         map[*ast.Ident]types.Object{},
		Uses:         map[*ast.Ident]types.Object{},
		Implicits:    map[ast.Node]types.Object{},
		Instances:    map[*ast.Ident]types.Instance{},
		Scopes:       map[ast.Node]*types.Scope{},
		Selections:   map[*ast.SelectorExpr]*types.Selection{},
		FileVersions: map[*ast.File]string{},
	}
	var typeErrs []types.Error
	tc := &types.Config{
		Importer: importerFunc(func(path string) (*types.Package, error) {
			if path == "unsafe" {
				return types.Unsafe, nil
			}
			ipkg := p.Imports[path]
			if ipkg == nil || ipkg.Types == nil {
				return nil, fmt.Errorf("no metadata for %s", path)
			}
			return ipkg.Types, nil
		}),
		Error: func(err error) {
			if te, ok := err.(types.Error); ok {
				typeErrs = append(typeErrs, te)
				errs = append(errs, packages.Error{
					Pos:  te.Fset.Position(te.Pos).String(),
					Msg:  te.Msg,
					Kind: packages.TypeError,
				})
			}
		},
		Sizes:            p.TypesSizes,
		IgnoreFuncBodies: dep,
	}
	if p.Module != nil && p.Module.GoVersion != "" {
		tc.GoVersion = "go" + p.Module.GoVersion
	}
	_ = types.NewChecker(tc, w.Fset, tpkg, info).Files(files)

	p.Fset, p.Syntax, p.Types, p.TypesInfo = w.Fset, files, tpkg, info
	p.TypeErrors, p.Errors = typeErrs, errs
	p.IllTyped = len(errs) > 0
}

// sameHeader reports whether two versions of a file declare the same
// package, import the same paths and have the same build constraints, so
// the package metadata from the last load still holds.
func sameHeader(a, b *ast.File) bool {
	if a.Name.Name != b.Name.Name {
		return false
	}
	importsOf := func(f *ast.File) []string {
		var out []string
		for _, s := range f.Imports {
			out = append(out, s.Path.Value)
		}
		sort.Strings(out)
		return out
	}
	if strings.Join(importsOf(a), "\n") != strings.Join(importsOf(b), "\n") {
		return false
	}
	return constraints(a) == constraints(b)
}

// constraints returns the build constraint lines that precede the package
// clause.
func constraints(f *ast.File) string {
	var out []string
	for _, cg := range f.Comments {
		if cg.Pos() >= f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "//go:build") || strings.HasPrefix(c.Text, "// +build") {
				out = append(out, c.Text)
			}
		}
	}
	return strings.Join(out, "\n")
}

// matchFile reports whether the build would include the file at path.
func matchFile(ctxt *build.Context, path string) bool {
	ok, err := ctxt.MatchFile(filepath.Dir(path), filepath.Base(path))
	return err != nil || ok // when unsure, assume it matters
}

// parseErrorsOf converts a parser error to go/packages errors, as
// go/packages does.
func parseErrorsOf(err error) []packages.Error {
	var list scanner.ErrorList
	if errors.As(err, &list) {
		var out []packages.Error
		for _, e := range list {
			out = append(out, packages.Error{Pos: e.Pos.String(), Msg: e.Msg, Kind: packages.ParseError})
		}
		return out
	}
	return []packages.Error{{Pos: "-", Msg: err.Error(), Kind: packages.ParseError}}
}

// errorFile returns the file name in a "file:line:col" position.
func errorFile(pos string) string {
	for i := 0; i < 2; i++ {
		if j := strings.LastIndexByte(pos, ':'); j >= 0 {
			pos = pos[:j]
		}
	}
	return pos
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }
