// Package tsapi is this module's one door to the TypeScript compiler (the
// Go port, TypeScript 7, through buke/typescript-go-internal): projects
// loaded from a tsconfig, kept up to date as files change, and their
// diagnostics. Nothing else imports the compiler, so that when it has an
// official Go API only this package changes.
package tsapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/bundled"
	"github.com/buke/typescript-go-internal/v7/pkg/compiler"
	"github.com/buke/typescript-go-internal/v7/pkg/core"
	"github.com/buke/typescript-go-internal/v7/pkg/diagnosticwriter"
	"github.com/buke/typescript-go-internal/v7/pkg/execute/incremental"
	"github.com/buke/typescript-go-internal/v7/pkg/locale"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
	"github.com/buke/typescript-go-internal/v7/pkg/tsoptions"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs/osvfs"
)

// Diagnostic is an error the compiler reports.
type Diagnostic struct {
	File         string // absolute; "" for one about no file (the config, a missing library)
	Line, Column int    // 1-based, the column in UTF-16 code units as editors count
	Message      string // one line, with its chain of reasons
}

// String is "file:line:col: message", as the engine compares errors.
func (d Diagnostic) String() string {
	if d.File == "" {
		return d.Message
	}
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Line, d.Column, d.Message)
}

// Project is one tsconfig's program.
type Project struct {
	Config string // absolute path of its tsconfig

	dir    string
	fs     vfs.FS
	parsed *tsoptions.ParsedCommandLine
	prog   *incremental.Program
}

type parseHost struct {
	fs  vfs.FS
	cwd string
}

func (h parseHost) FS() vfs.FS                  { return h.fs }
func (h parseHost) GetCurrentDirectory() string { return h.cwd }

func fileSystem() vfs.FS { return bundled.WrapFS(osvfs.FS()) }

func parse(fs vfs.FS, config string) (*tsoptions.ParsedCommandLine, error) {
	config = tspath.NormalizePath(config)
	parsed, errs := tsoptions.GetParsedCommandLineOfConfigFile(config, nil, nil, parseHost{fs, tspath.GetDirectoryPath(config)}, nil)
	if parsed == nil {
		return nil, fmt.Errorf("%s: %s", config, messages(errs))
	}
	if errs := parsed.GetConfigFileParsingDiagnostics(); len(errs) > 0 {
		return nil, fmt.Errorf("%s: %s", config, messages(errs))
	}
	return parsed, nil
}

func messages(ds []*ast.Diagnostic) string {
	var out []string
	for _, d := range ds {
		out = append(out, flatten(d))
	}
	return strings.Join(out, "; ")
}

// Solution returns the tsconfigs that config stands for: itself, when it
// names files of its own, and every project it references, transitively,
// that does. A file's default project may leave out some of the files
// that use it (tests), so a workspace loads them all.
func Solution(config string) ([]string, error) {
	fs := fileSystem()
	var out []string
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(c string) error {
		c = tspath.NormalizePath(c)
		if seen[c] {
			return nil
		}
		seen[c] = true
		parsed, err := parse(fs, c)
		if err != nil {
			return err
		}
		if len(parsed.FileNames()) > 0 {
			out = append(out, c)
		}
		for _, ref := range parsed.ProjectReferences() {
			if err := visit(core.ResolveConfigFileNameOfProjectReference(core.ResolveProjectReferencePath(ref))); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(config); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s names no files, and no project it references does", config)
	}
	return out, nil
}

// Load loads the project of config.
func Load(config string) (*Project, error) {
	config = tspath.NormalizePath(config)
	p := &Project{Config: config, dir: tspath.GetDirectoryPath(config), fs: fileSystem()}
	return p, p.Reload()
}

func (p *Project) host() compiler.CompilerHost {
	return compiler.NewCompilerHost(p.dir, p.fs, bundled.LibPath(), nil, nil)
}

// Reload parses the config again and builds the program anew, keeping
// what the old one checked for files that did not change.
func (p *Project) Reload() error {
	parsed, err := parse(p.fs, p.Config)
	if err != nil {
		return err
	}
	host := p.host()
	prog := compiler.NewProgram(compiler.ProgramOptions{Host: host, Config: parsed})
	p.parsed = parsed
	p.prog = incremental.NewProgram(prog, p.prog, incremental.CreateHost(host), false)
	return nil
}

// Has reports whether the program has the file at path.
func (p *Project) Has(path string) bool {
	return p.prog.GetProgram().GetSourceFile(tspath.NormalizePath(path)) != nil
}

// Matches reports whether the config's include and exclude take the
// file at path, which may not exist yet.
func (p *Project) Matches(path string) bool {
	path = tspath.NormalizePath(path)
	if p.Has(path) {
		return true
	}
	parsed, err := parse(p.fs, p.Config)
	if err != nil {
		return false
	}
	return slices.Contains(parsed.FileNames(), path)
}

// Update brings the program up to date after files changed on disk:
// each that it has and that is still there is replaced in place;
// anything else (a file created or deleted) rebuilds it.
func (p *Project) Update(paths []string) error {
	prog := p.prog.GetProgram()
	host := p.host()
	for _, path := range paths {
		path = tspath.NormalizePath(path)
		if prog.GetSourceFile(path) == nil {
			if p.Matches(path) {
				return p.Reload()
			}
			continue
		}
		if !p.fs.FileExists(path) {
			return p.Reload()
		}
		next, file, _ := prog.UpdateProgram(tspath.ToPath(path, p.dir, p.fs.UseCaseSensitiveFileNames()), host, nil)
		if file == nil {
			return p.Reload()
		}
		prog = next
	}
	p.prog = incremental.NewProgram(prog, p.prog, incremental.CreateHost(host), false)
	return nil
}

// Diagnostics returns the program's syntactic, semantic and global
// errors in the files it has under dir, and those about no file.
func (p *Project) Diagnostics(ctx context.Context, dir string) []Diagnostic {
	dir = tspath.NormalizePath(dir)
	var out []Diagnostic
	add := func(ds []*ast.Diagnostic) {
		for _, d := range ds {
			out = append(out, convert(d))
		}
	}
	add(p.prog.GetProgramDiagnostics())
	add(p.prog.GetGlobalDiagnostics(ctx))
	for _, f := range p.prog.GetSourceFiles() {
		name := f.FileName()
		if !strings.HasPrefix(name, dir+"/") || strings.Contains(name, "/node_modules/") || f.IsDeclarationFile {
			continue
		}
		add(p.prog.GetSyntacticDiagnostics(ctx, f))
		add(p.prog.GetSemanticDiagnostics(ctx, f))
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		out = append(out, Diagnostic{Message: err.Error()})
	}
	return out
}

func convert(d *ast.Diagnostic) Diagnostic {
	out := Diagnostic{Message: flatten(d)}
	if f := d.File(); f != nil {
		line, col := scanner.GetECMALineAndUTF16CharacterOfPosition(f, d.Pos())
		out.File, out.Line, out.Column = f.FileName(), line+1, int(col)+1
	}
	return out
}

func flatten(d *ast.Diagnostic) string {
	return diagnosticwriter.FlattenDiagnosticMessage(diagnosticwriter.WrapASTDiagnostic(d), " ", locale.Default)
}
