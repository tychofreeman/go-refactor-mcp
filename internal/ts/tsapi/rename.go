package tsapi

import (
	"context"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
	"github.com/buke/typescript-go-internal/v7/pkg/compiler"
	"github.com/buke/typescript-go-internal/v7/pkg/core"
	"github.com/buke/typescript-go-internal/v7/pkg/ls"
	"github.com/buke/typescript-go-internal/v7/pkg/ls/autoimport"
	"github.com/buke/typescript-go-internal/v7/pkg/ls/lsconv"
	"github.com/buke/typescript-go-internal/v7/pkg/ls/lsutil"
	"github.com/buke/typescript-go-internal/v7/pkg/lsp/lsproto"
	"github.com/buke/typescript-go-internal/v7/pkg/scanner"
	"github.com/buke/typescript-go-internal/v7/pkg/sourcemap"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs/vfsmatch"
)

// Ident is one identifier in a file.
type Ident struct {
	Name         string
	Offset       int // byte offset of its first character
	Line, Column int // 1-based; the column in bytes
}

// Edit replaces the bytes [Start, End) of File with NewText.
type Edit struct {
	File       string // absolute
	Start, End int
	NewText    string
}

func (p *Project) sourceFile(path string) *ast.SourceFile {
	return p.prog.GetProgram().GetSourceFile(tspath.NormalizePath(path))
}

// Identifiers returns every identifier of the file at path, in order, or
// nil if the program does not have the file.
func (p *Project) Identifiers(path string) []Ident {
	f := p.sourceFile(path)
	if f == nil {
		return nil
	}
	var out []Ident
	text := f.Text()
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		if ast.IsIdentifier(n) {
			off := scanner.GetTokenPosOfNode(n, f, false)
			line := strings.Count(text[:off], "\n")
			col := off - (strings.LastIndex(text[:off], "\n") + 1)
			out = append(out, Ident{Name: n.Text(), Offset: off, Line: line + 1, Column: col + 1})
		}
		n.ForEachChild(visit)
		return false
	}
	f.AsNode().ForEachChild(visit)
	return out
}

// TopLevel returns the offset of the name of the file's top-level
// declaration called name: a function, class, interface, type alias,
// enum or variable.
func (p *Project) TopLevel(path, name string) (int, bool) {
	f := p.sourceFile(path)
	if f == nil {
		return 0, false
	}
	check := func(n *ast.Node) (int, bool) {
		if n != nil && ast.IsIdentifier(n) && n.Text() == name {
			return scanner.GetTokenPosOfNode(n, f, false), true
		}
		return 0, false
	}
	for _, s := range f.Statements.Nodes {
		if ast.IsVariableStatement(s) {
			for _, d := range s.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				if off, ok := check(d.Name()); ok {
					return off, true
				}
			}
			continue
		}
		if off, ok := check(s.Name()); ok {
			return off, true
		}
	}
	return 0, false
}

// Rename renames the symbol at offset in the file at path to newName in
// this project, through the language service. refusal is why it cannot,
// in the service's words, when it cannot.
func (p *Project) Rename(path string, offset int, newName string) (edits []Edit, refusal string, err error) {
	path = tspath.NormalizePath(path)
	prog := p.prog.GetProgram()
	f := prog.GetSourceFile(path)
	if f == nil {
		return nil, path + " is not in " + p.Config, nil
	}
	h := newHost(p.fs, prog)
	svc := ls.NewLanguageService(tspath.Path(p.Config), prog, h, path)
	uri := lsconv.FileNameToDocumentURI(path)
	pos := h.position(path, offset)
	ctx := context.Background()
	if info := svc.GetRenameInfo(ctx, newName, uri, pos); !info.CanRename {
		return nil, info.LocalizedErrorMessage, nil
	}
	res, err := svc.ProvideRename(ctx, &lsproto.RenameParams{TextDocument: lsproto.TextDocumentIdentifier{Uri: uri}, Position: pos, NewName: newName}, nil)
	if err != nil || res.WorkspaceEdit == nil || res.WorkspaceEdit.Changes == nil {
		return nil, "", err
	}
	for u, tes := range *res.WorkspaceEdit.Changes {
		file := u.FileName()
		for _, te := range tes {
			edits = append(edits, Edit{File: file, Start: h.offset(file, te.Range.Start), End: h.offset(file, te.Range.End), NewText: te.NewText})
		}
	}
	return edits, "", nil
}

// host is what the language service reads of the program and the files,
// with positions as line and byte column (UTF-8).
type host struct {
	fs    vfs.FS
	prog  *compiler.Program
	conv  *lsconv.Converters
	lines map[string]*lsconv.LSPLineMap
}

func newHost(fs vfs.FS, prog *compiler.Program) *host {
	h := &host{fs: fs, prog: prog, lines: map[string]*lsconv.LSPLineMap{}}
	h.conv = lsconv.NewConverters(lsproto.PositionEncodingKindUTF8, h.lineMap)
	return h
}

func (h *host) text(fileName string) string {
	if f := h.prog.GetSourceFile(fileName); f != nil {
		return f.Text()
	}
	s, _ := h.fs.ReadFile(fileName)
	return s
}

func (h *host) lineMap(fileName string) *lsconv.LSPLineMap {
	if m, ok := h.lines[fileName]; ok {
		return m
	}
	m := lsconv.ComputeLSPLineStarts(h.text(fileName))
	h.lines[fileName] = m
	return m
}

// position is offset in fileName as a line and byte column.
func (h *host) position(fileName string, offset int) lsproto.Position {
	text := h.text(fileName)
	line := strings.Count(text[:offset], "\n")
	return lsproto.Position{Line: uint32(line), Character: uint32(offset - (strings.LastIndex(text[:offset], "\n") + 1))}
}

// offset is the byte offset of a line and byte column in fileName.
func (h *host) offset(fileName string, p lsproto.Position) int {
	text := h.text(fileName)
	off := 0
	for range p.Line {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	return min(off+int(p.Character), len(text))
}

func (h *host) UseCaseSensitiveFileNames() bool { return h.fs.UseCaseSensitiveFileNames() }
func (h *host) ReadFile(path string) (string, bool) {
	if f := h.prog.GetSourceFile(path); f != nil {
		return f.Text(), true
	}
	return h.fs.ReadFile(path)
}
func (h *host) Converters() *lsconv.Converters { return h.conv }
func (h *host) GetPreferences(string) lsutil.UserPreferences {
	return lsutil.NewDefaultUserPreferences()
}
func (h *host) GetECMALineInfo(fileName string) *sourcemap.ECMALineInfo {
	text := h.text(fileName)
	return sourcemap.CreateECMALineInfo(text, core.ComputeECMALineStarts(text))
}
func (h *host) AutoImportRegistry() *autoimport.Registry { return nil }
func (h *host) ReadDirectory(currentDir, path string, extensions, excludes, includes []string, depth int) []string {
	return vfsmatch.ReadDirectory(h.fs, currentDir, path, extensions, excludes, includes, depth)
}
func (h *host) GetDirectories(path string) []string {
	return h.fs.GetAccessibleEntries(path).Directories
}
func (h *host) DirectoryExists(path string) bool { return h.fs.DirectoryExists(path) }
func (h *host) FileExists(path string) bool      { return h.fs.FileExists(path) }
