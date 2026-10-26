package analysis

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// unit is a type-checked package of the differential test's corpus.
type unit struct {
	name  string
	fset  *token.FileSet
	files []*ast.File
	info  *types.Info
}

func newInfo() *types.Info {
	return &types.Info{
		Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits: map[ast.Node]types.Object{}, Scopes: map[ast.Node]*types.Scope{},
		FileVersions: map[*ast.File]string{},
	}
}

// goldens returns the packages of the goldens' input modules that
// type-check with the standard library alone.
func goldens(t *testing.T) []unit {
	var dirs []string
	filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && strings.Contains(path, "/testdata/") && strings.Contains(path, "/in/") {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	sort.Strings(dirs)
	imp := importer.Default()
	var units []unit
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		for name, p := range pkgs {
			if strings.HasSuffix(name, "_test") {
				continue
			}
			var files []*ast.File
			for _, f := range p.Files {
				files = append(files, f)
			}
			info := newInfo()
			conf := types.Config{Importer: imp, Error: func(error) {}}
			if _, err := conf.Check(name, fset, files, info); err != nil {
				continue
			}
			units = append(units, unit{dir, fset, files, info})
		}
	}
	return units
}

// module returns the packages of the module at dir.
func module(t *testing.T, dir string) []unit {
	cfg := &packages.Config{Dir: dir, Tests: true, Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatal(err)
	}
	var units []unit
	for _, p := range pkgs {
		if len(p.Errors) > 0 || p.TypesInfo == nil {
			continue
		}
		units = append(units, unit{p.ID, p.Fset, p.Syntax, p.TypesInfo})
	}
	return units
}

// verdicts is what one question asks of an expression, old and new.
type verdicts struct {
	name     string
	old, new func(f *Func, x ast.Expr) bool
}

// TestDifferential compares what Stable, MayPanic and HasEffect said of
// every expression of the goldens, of this module (unless -short) and of
// the module at DIFF_DIR, with what the summaries say, for the questions
// callers ask of them. Every difference must have a reason (explain).
func TestDifferential(t *testing.T) {
	units := goldens(t)
	if !testing.Short() {
		units = append(units, module(t, "../..")...)
	}
	if dir := os.Getenv("DIFF_DIR"); dir != "" {
		units = append(units, module(t, dir)...)
	}
	effects := NewEffects()
	type question struct {
		name     string
		old, new func(f *Func, x ast.Expr) bool
	}
	var qs []question
	for _, sharing := range []bool{false, true} {
		qs = append(qs,
			question{fmt.Sprintf("Stable(sharing=%v)", sharing),
				func(f *Func, x ast.Expr) bool { _, ok := f.Stable(x, sharing); return ok },
				func(f *Func, x ast.Expr) bool { _, ok := effects.Of(f, x).Stable(sharing); return ok }},
			question{fmt.Sprintf("Stable and MayPanic(sharing=%v)", sharing),
				func(f *Func, x ast.Expr) bool { _, ok := f.Stable(x, sharing); return ok && MayPanic(f.Info, x) == nil },
				func(f *Func, x ast.Expr) bool {
					s := effects.Of(f, x)
					_, ok := s.Stable(sharing)
					return ok && s.At.Panic == nil
				}})
	}
	qs = append(qs,
		question{"HasEffect or MayPanic",
			func(f *Func, x ast.Expr) bool { return HasEffect(f.Info, x) || MayPanic(f.Info, x) != nil },
			func(f *Func, x ast.Expr) bool {
				s := effects.Of(Outside(f.Info), x)
				return s.Effect != "" || s.At.OtherPanic != nil || s.Makes != ""
			}},
		question{"HasEffect",
			func(f *Func, x ast.Expr) bool { return HasEffect(f.Info, x) },
			func(f *Func, x ast.Expr) bool { return effects.Acts(f.Info, x) }})
	// explain returns why a difference is right, or "".
	explain := func(f *Func, q question, x ast.Expr, old bool) string {
		info := f.Info
		var fmtCall, makes, acts bool
		ast.Inspect(x, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if fn, ok := info.Uses[IdentOf(c.Fun)].(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" {
					fmtCall = true
				}
				if b, ok := info.Uses[IdentOf(c.Fun)].(*types.Builtin); ok && b.Name() == "make" {
					makes = true
				}
			}
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			acts = acts || HasEffect(info, n)
			return true
		})
		// permissive: the new verdict lets more through.
		permissive := !old
		if strings.HasPrefix(q.name, "HasEffect") {
			permissive = old
		}
		switch {
		case permissive && fmtCall:
			return "Effects knows fmt calls of values without methods"
		case permissive && strings.Contains(q.name, "sharing=false") && effects.Of(f, x).Makes != "":
			return "without sharing, a new reference value is stable"
		case !permissive && makes:
			return "make of a size not known to be in range can panic"
		case !permissive && q.name == "HasEffect or MayPanic" && acts:
			return "a call inside x acts: HasEffect looked at x alone"
		case !permissive && q.name == "HasEffect or MayPanic" && effects.Of(f, x).Makes != "":
			return "evaluated another number of times, a new reference value is another value"
		case permissive && q.name == "HasEffect or MayPanic" && effects.Of(Outside(info), x).Reads != "":
			return "a call that only reads, as a read through a pointer, which HasEffect did not count either"
		case permissive && q.name == "HasEffect or MayPanic" && effects.Of(Outside(info), x).Panics == NilPanic:
			return "a call that can panic only by a nil dereference, which MayPanic did not count either"
		case permissive && q.name == "HasEffect" && effects.Of(f, x).Makes != "":
			return "a call that makes a new value, and does nothing else, does not act"
		case permissive && strings.HasPrefix(q.name, "Stable(") && effects.Of(f, x).Panics != NoPanic:
			return "a call that can panic is stable; At.OtherPanic says that it can"
		}
		return ""
	}
	type diff struct {
		count   int
		example string
	}
	diffs, explained := map[string]*diff{}, map[string]int{}
	exprs := 0
	for _, u := range units {
		for _, file := range u.files {
			for _, d := range file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				f := AnalyzeFunc(u.info, fd)
				callee := map[ast.Expr]bool{}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok {
						callee[ast.Unparen(c.Fun)] = true
					}
					x, ok := n.(ast.Expr)
					if !ok || callee[x] {
						return true
					}
					if tv, ok := u.info.Types[x]; !ok || !tv.IsValue() {
						return true
					}
					exprs++
					for _, q := range qs {
						o, nw := q.old(f, x), q.new(f, x)
						if o == nw {
							continue
						}
						if why := explain(f, q, x, o); why != "" {
							explained[q.name+": "+why]++
							continue
						}
						key := fmt.Sprintf("%s: old %v, new %v, %T", q.name, o, nw, x)
						if diffs[key] == nil {
							diffs[key] = &diff{example: u.fset.Position(x.Pos()).String() + ": " + types.ExprString(x)}
						}
						diffs[key].count++
					}
					return true
				})
			}
		}
	}
	t.Logf("%d packages, %d expressions", len(units), exprs)
	for _, k := range slices.Sorted(maps.Keys(explained)) {
		t.Logf("%s: %d", k, explained[k])
	}
	for _, k := range slices.Sorted(maps.Keys(diffs)) {
		t.Errorf("%s: %d, e.g. %s", k, diffs[k].count, diffs[k].example)
	}
}
