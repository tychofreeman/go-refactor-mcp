package main

import (
	"context"
	"fmt"
	"iter"
	"sort"
	"strings"
	"time"

	"github.com/buke/typescript-go-internal/v7/pkg/bundled"
	"github.com/buke/typescript-go-internal/v7/pkg/collections"
	"github.com/buke/typescript-go-internal/v7/pkg/compiler"
	"github.com/buke/typescript-go-internal/v7/pkg/execute/incremental"
	"github.com/buke/typescript-go-internal/v7/pkg/ls"
	"github.com/buke/typescript-go-internal/v7/pkg/ls/lsconv"
	"github.com/buke/typescript-go-internal/v7/pkg/lsp/lsproto"
	"github.com/buke/typescript-go-internal/v7/pkg/project"
	"github.com/buke/typescript-go-internal/v7/pkg/tspath"
	"github.com/buke/typescript-go-internal/v7/pkg/vfs"
)

// orchestrator lets a rename reach every project of the session, as the
// language server's does.
type orchestrator struct {
	s   *project.Session
	def *project.Project
	all []ls.Project
}

func (o *orchestrator) GetDefaultProject() ls.Project                 { return o.def }
func (o *orchestrator) GetAllProjectsForInitialRequest() []ls.Project { return o.all }
func (o *orchestrator) GetLanguageServiceForProjectWithFile(ctx context.Context, p ls.Project, uri lsproto.DocumentUri) *ls.LanguageService {
	return o.s.GetLanguageServiceForProjectWithFile(ctx, p.(*project.Project), uri)
}
func (o *orchestrator) GetProjectsForFile(ctx context.Context, uri lsproto.DocumentUri) ([]ls.Project, error) {
	return o.s.GetProjectsForFile(ctx, uri)
}
func (o *orchestrator) GetProjectsLoadingProjectTree(ctx context.Context, trees *collections.Set[tspath.Path]) iter.Seq[ls.Project] {
	return func(yield func(ls.Project) bool) {
		o.s.WithSnapshotLoadingProjectTree(ctx, trees, func(snap *project.Snapshot) {
			for _, p := range snap.ProjectCollection.Projects() {
				if !yield(p) {
					return
				}
			}
		})
	}
}

// renameAt renames the identifier at offset in abs to name through the
// language server's session, applies the edits to ov, and checks the
// project before and after with a full program, which must report the
// same diagnostics.
func renameAt(ctx context.Context, base vfs.FS, cwd, cfgPath, abs string, offset int, name string, ov overlay, alsoOpen []string) {
	t := time.Now()
	s := project.NewSession(&project.SessionInit{
		BackgroundCtx: ctx,
		Options: &project.SessionOptions{
			CurrentDirectory:   cwd,
			DefaultLibraryPath: bundled.LibPath(),
			PositionEncoding:   lsproto.PositionEncodingKindUTF8,
		},
		FS: base,
	})
	defer s.Close()
	src, _ := base.ReadFile(abs)
	uri := lsconv.FileNameToDocumentURI(abs)
	s.DidOpenFile(ctx, uri, 1, src, lsproto.LanguageKindTypeScript)
	for _, f := range alsoOpen {
		text, _ := base.ReadFile(f)
		s.DidOpenFile(ctx, lsconv.FileNameToDocumentURI(f), 1, text, lsproto.LanguageKindTypeScript)
	}
	def, lsvc, all, err := s.GetLanguageServiceAndProjectsForFile(ctx, uri)
	if err != nil {
		fmt.Println("session:", err)
		return
	}
	var names []string
	for _, p := range all {
		names = append(names, p.(*project.Project).Name())
	}
	fmt.Printf("session: project %s in %v; projects with the file: %v\n", def.Name(), time.Since(t).Round(time.Millisecond), names)

	line, char := position(src, offset)
	t = time.Now()
	res, err := lsvc.ProvideRename(ctx, &lsproto.RenameParams{
		TextDocument: lsproto.TextDocumentIdentifier{Uri: uri},
		Position:     lsproto.Position{Line: uint32(line), Character: uint32(char)},
		NewName:      name,
	}, &orchestrator{s, def, all})
	if err != nil || res.WorkspaceEdit == nil || res.WorkspaceEdit.Changes == nil {
		fmt.Println("rename:", err, "no edits")
		return
	}
	changes := *res.WorkspaceEdit.Changes
	n := 0
	var files []string
	for u, edits := range changes {
		n += len(edits)
		f := u.FileName()
		files = append(files, fmt.Sprintf("%s (%d)", strings.TrimPrefix(f, cwd+"/"), len(edits)))
		text, ok := ov[f]
		if !ok {
			text, _ = base.ReadFile(f)
		}
		ov[f] = apply(text, edits)
	}
	sort.Strings(files)
	fmt.Printf("rename: %d edits in %d files in %v: %s\n", n, len(changes), time.Since(t).Round(time.Millisecond), strings.Join(files, ", "))

	parsed := loadConfig(base, cwd, cfgPath)
	check := func(fs vfs.FS) []string {
		host := compiler.NewCompilerHost(cwd, fs, bundled.LibPath(), nil, nil)
		p := compiler.NewProgram(compiler.ProgramOptions{Host: host, Config: parsed})
		return diagnose(ctx, p, p.SourceFiles())
	}
	before := check(base)
	after := check(ov.fs(base))
	added, removed := compare(before, after)
	fmt.Printf("after rename: diagnostics +%d -%d\n", len(added), len(removed))
	for _, d := range added {
		fmt.Println("  +", d)
	}

	// The same check through the incremental builder: the program before,
	// checked once, then every changed file updated in turn and the
	// result re-checked. Run twice, the first time to record signatures.
	host := compiler.NewCompilerHost(cwd, base, bundled.LibPath(), nil, nil)
	ip := incremental.NewProgram(compiler.NewProgram(compiler.ProgramOptions{Host: host, Config: parsed}), nil, incremental.CreateHost(host), false)
	ip.GetSemanticDiagnostics(ctx, nil)
	orig := overlay{}
	for f := range ov {
		orig[f], _ = base.ReadFile(f)
	}
	for i, target := range []overlay{ov, orig, ov} {
		t := time.Now()
		h := compiler.NewCompilerHost(cwd, target.fs(base), bundled.LibPath(), nil, nil)
		p := ip.GetProgram()
		for f := range target {
			p, _, _ = p.UpdateProgram(tspath.ToPath(f, cwd, base.UseCaseSensitiveFileNames()), h, nil)
		}
		ip = incremental.NewProgram(p, ip, incremental.CreateHost(h), false)
		n := len(ip.GetSemanticDiagnostics(ctx, nil))
		fmt.Printf("incremental re-check %d (%s): %d diagnostics in %v\n", i+1, map[int]string{0: "renamed", 1: "back", 2: "renamed"}[i], n, time.Since(t).Round(time.Millisecond))
	}
}

// position is the zero-based line and byte column of offset in src.
func position(src string, offset int) (int, int) {
	line := strings.Count(src[:offset], "\n")
	return line, offset - (strings.LastIndex(src[:offset], "\n") + 1)
}

// apply applies edits, given as line and byte column ranges, to text.
func apply(text string, edits []*lsproto.TextEdit) string {
	starts := []int{0}
	for i, c := range text {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	off := func(p lsproto.Position) int { return starts[p.Line] + int(p.Character) }
	sorted := append([]*lsproto.TextEdit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool { return off(sorted[i].Range.Start) > off(sorted[j].Range.Start) })
	for _, e := range sorted {
		text = text[:off(e.Range.Start)] + e.NewText + text[off(e.Range.End):]
	}
	return text
}
