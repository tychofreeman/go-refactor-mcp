// Command ts-spike measures whether the Go port of the TypeScript compiler
// can carry the refactoring engine: load a tsconfig into a Program over an
// overlay file system, change a file in memory, update the Program, and
// re-check only the files that import the changed one. Throwaway code for
// docs/ts-plan.md step 0.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/bundled"
	"github.com/buke/typescript-go-internal/v7/pkg/compiler"
	"github.com/buke/typescript-go-internal/v7/pkg/execute/incremental"
	"github.com/buke/typescript-go-internal/v7/pkg/tsoptions"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs/osvfs"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs/wrapvfs"
)

// overlay holds file contents that replace what is on disk.
type overlay map[string]string

func (o overlay) fs(base vfs.FS) vfs.FS {
	return wrapvfs.Wrap(base, wrapvfs.Replacements{
		ReadFile: func(p string) (string, bool) {
			if s, ok := o[p]; ok {
				return s, true
			}
			return base.ReadFile(p)
		},
	})
}

type parseHost struct {
	fs  vfs.FS
	cwd string
}

func (h parseHost) FS() vfs.FS                  { return h.fs }
func (h parseHost) GetCurrentDirectory() string { return h.cwd }

func main() {
	config := flag.String("p", "tsconfig.json", "tsconfig to load")
	file := flag.String("file", "", "file to change, relative to the config's directory")
	find := flag.String("find", "", "text in -file to replace")
	repl := flag.String("replace", "", "replacement text")
	rename := flag.String("rename", "", "rename the identifier at -find in -file to this name, through the language service")
	open := flag.String("open", "", "comma-separated files to open in the session too, relative to the config's directory")
	inc := flag.Bool("inc", false, "re-check through the incremental builder (tsc --watch's), not every importer")
	flag.Parse()

	ctx := context.Background()
	cfgPath, _ := filepath.Abs(*config)
	cwd := filepath.Dir(cfgPath)
	base := bundled.WrapFS(osvfs.FS())
	ov := overlay{}

	t := time.Now()
	if *rename != "" {
		abs := tspath.NormalizePath(filepath.Join(cwd, *file))
		src, _ := base.ReadFile(abs)
		var also []string
		for _, f := range strings.Split(*open, ",") {
			if f != "" {
				also = append(also, tspath.NormalizePath(filepath.Join(cwd, f)))
			}
		}
		renameAt(ctx, base, cwd, cfgPath, abs, strings.Index(src, *find), *rename, ov, also)
		return
	}
	parsed := loadConfig(base, cwd, cfgPath)
	host := compiler.NewCompilerHost(cwd, ov.fs(base), bundled.LibPath(), nil, nil)
	prog := compiler.NewProgram(compiler.ProgramOptions{Host: host, Config: parsed})
	fmt.Printf("load: %d files (%d under root) in %v\n", len(prog.SourceFiles()), len(rootFiles(prog, cwd)), time.Since(t).Round(time.Millisecond))

	t = time.Now()
	before := diagnose(ctx, prog, rootFiles(prog, cwd))
	fmt.Printf("check all: %d diagnostics in %v\n", len(before), time.Since(t).Round(time.Millisecond))

	if *file == "" {
		return
	}
	abs := tspath.NormalizePath(filepath.Join(cwd, *file))
	src, ok := base.ReadFile(abs)
	if !ok || !strings.Contains(src, *find) {
		fmt.Println("text not found in", abs)
		os.Exit(1)
	}

	if *inc {
		incrementalRounds(ctx, prog, base, cwd, ov, abs, src, strings.Replace(src, *find, *repl, 1))
		return
	}
	for round := 1; round <= 3; round++ {
		ov[abs] = strings.Replace(src, *find, *repl, 1)
		if round == 2 { // and back, to see the updated program reused again
			ov[abs] = src
		}
		t = time.Now()
		host = compiler.NewCompilerHost(cwd, ov.fs(base), bundled.LibPath(), nil, nil)
		path := tspath.ToPath(abs, cwd, base.UseCaseSensitiveFileNames())
		next, _, reused := prog.UpdateProgram(path, host, nil)
		upd := time.Since(t)

		t = time.Now()
		affected := importers(next, path)
		dep := time.Since(t)

		t = time.Now()
		after := diagnose(ctx, next, affected)
		chk := time.Since(t)

		was := diagnose(ctx, prog, same(prog, affected)) // the same files before, for the comparison (not timed)
		added, removed := compare(was, after)
		fmt.Printf("round %d: update %v (reused %v), importers %d in %v, check them %v; diagnostics +%d -%d\n",
			round, upd.Round(time.Millisecond), reused, len(affected), dep.Round(time.Millisecond), chk.Round(time.Millisecond), len(added), len(removed))
		for i, d := range added {
			if i == 5 {
				fmt.Printf("  ... %d more\n", len(added)-5)
				break
			}
			fmt.Println("  +", d)
		}
		prog = next
	}
}

func loadConfig(base vfs.FS, cwd, cfgPath string) *tsoptions.ParsedCommandLine {
	parsed, errs := tsoptions.GetParsedCommandLineOfConfigFile(cfgPath, nil, nil, parseHost{base, cwd}, nil)
	if len(errs) > 0 {
		fmt.Println("config errors:", len(errs))
	}
	return parsed
}

// incrementalRounds changes the file, changes it back, and changes it
// again, re-checking through the incremental builder each time: it keeps
// each file's diagnostics and re-checks a file's importers only when its
// declaration output changed.
func incrementalRounds(ctx context.Context, prog *compiler.Program, base vfs.FS, cwd string, ov overlay, abs, orig, changed string) {
	host := compiler.NewCompilerHost(cwd, ov.fs(base), bundled.LibPath(), nil, nil)
	t := time.Now()
	ip := incremental.NewProgram(prog, nil, incremental.CreateHost(host), false)
	before := keys(ip.GetSemanticDiagnostics(ctx, nil))
	fmt.Printf("incremental: first check %d diagnostics in %v\n", len(before), time.Since(t).Round(time.Millisecond))
	path := tspath.ToPath(abs, cwd, base.UseCaseSensitiveFileNames())
	for round, text := range []string{changed, orig, changed} {
		ov[abs] = text
		t = time.Now()
		host = compiler.NewCompilerHost(cwd, ov.fs(base), bundled.LibPath(), nil, nil)
		next, _, reused := ip.GetProgram().UpdateProgram(path, host, nil)
		nip := incremental.NewProgram(next, ip, incremental.CreateHost(host), false)
		after := keys(nip.GetSemanticDiagnostics(ctx, nil))
		added, removed := compare(before, after)
		fmt.Printf("round %d: update and re-check %v (reused %v); diagnostics +%d -%d\n",
			round+1, time.Since(t).Round(time.Millisecond), reused, len(added), len(removed))
		ip, before = nip, after
	}
}

func keys(ds []*ast.Diagnostic) []string {
	var out []string
	for _, d := range ds {
		out = append(out, key(d))
	}
	sort.Strings(out)
	return out
}

// rootFiles is the program's files under dir, not in node_modules.
func rootFiles(p *compiler.Program, dir string) []*ast.SourceFile {
	var out []*ast.SourceFile
	for _, f := range p.SourceFiles() {
		n := f.FileName()
		if strings.HasPrefix(n, tspath.NormalizePath(dir)+"/") && !strings.Contains(n, "/node_modules/") {
			out = append(out, f)
		}
	}
	return out
}

// same is files as p holds them, by path.
func same(p *compiler.Program, files []*ast.SourceFile) []*ast.SourceFile {
	var out []*ast.SourceFile
	for _, f := range files {
		if g := p.GetSourceFileByPath(f.Path()); g != nil {
			out = append(out, g)
		}
	}
	return out
}

// importers is the changed file and every file that imports it, directly
// or through others.
func importers(p *compiler.Program, changed tspath.Path) []*ast.SourceFile {
	byPath := map[tspath.Path]*ast.SourceFile{}
	for _, f := range p.SourceFiles() {
		byPath[f.Path()] = f
	}
	rev := map[tspath.Path][]tspath.Path{}
	for from, cache := range p.GetResolvedModules() {
		for _, r := range cache {
			if r == nil || r.ResolvedFileName == "" {
				continue
			}
			to := tspath.ToPath(r.ResolvedFileName, p.GetCurrentDirectory(), true)
			rev[to] = append(rev[to], from)
		}
	}
	seen := map[tspath.Path]bool{changed: true}
	queue := []tspath.Path{changed}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, from := range rev[cur] {
			if !seen[from] {
				seen[from] = true
				queue = append(queue, from)
			}
		}
	}
	var out []*ast.SourceFile
	for path := range seen {
		if f := byPath[path]; f != nil {
			out = append(out, f)
		}
	}
	return out
}

// diagnose returns the syntactic and semantic diagnostics of files, each
// as a string that does not depend on its position.
func diagnose(ctx context.Context, p *compiler.Program, files []*ast.SourceFile) []string {
	var out []string
	for _, f := range files {
		for _, d := range append(p.GetSyntacticDiagnostics(ctx, f), p.GetSemanticDiagnostics(ctx, f)...) {
			out = append(out, key(d))
		}
	}
	sort.Strings(out)
	return out
}

func key(d *ast.Diagnostic) string {
	name := ""
	if d.File() != nil {
		name = filepath.Base(d.File().FileName())
	}
	return fmt.Sprintf("%s TS%d %s %v", name, d.Code(), d.MessageKey(), d.MessageArgs())
}

// compare returns what after has that before does not, and the reverse,
// counting duplicates.
func compare(before, after []string) (added, removed []string) {
	count := map[string]int{}
	for _, s := range before {
		count[s]++
	}
	for _, s := range after {
		if count[s] > 0 {
			count[s]--
		} else {
			added = append(added, s)
		}
	}
	for s, n := range count {
		for range n {
			removed = append(removed, s)
		}
	}
	return added, removed
}
