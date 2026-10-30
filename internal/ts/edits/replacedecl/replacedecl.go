// Package replacedecl is replace_declaration for TypeScript: an edit that replaces a declaration, found by name, with new source.
package replacedecl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// ReplaceDeclaration is replace_declaration.
type ReplaceDeclaration struct{}

// New returns replace_declaration.
func New() *ReplaceDeclaration { return &ReplaceDeclaration{} }

func (*ReplaceDeclaration) ChangesBehavior() {}

func (*ReplaceDeclaration) Name() string { return "replace_declaration" }

func (*ReplaceDeclaration) Description() string {
	return `Replace a whole declaration, found by name, with new source: an edit, for a signature and body that change together, or a declaration too long to restate as replace_text's old (a table, a type); for a body alone, use replace_statements with function. With name "Bed.depth" and content depth(extra: number): number { return this.base + extra; }, depth's signature and body are replaced together, indented as it was.
content is the new declaration; the old doc comment stays unless content has its own. Empty content deletes the declaration and its doc comment. Callers a new signature or a deletion breaks are refused, or fixed in the same batch with check "end".
Refuses, saying why: a name the file does not declare, or declares more than once (overloads, merged declarations).`
}

func (*ReplaceDeclaration) InputSchema() json.RawMessage {
	return refactor.Schema[replaceDeclarationArgs]()
}

// replaceDeclarationArgs are the arguments of replace_declaration.
type replaceDeclarationArgs struct {
	File    string `json:"file" desc:"The file that declares it, relative to the root."`
	Name    string `json:"name" desc:"The declared name: name, or Class.member for a member."`
	Content string `json:"content" desc:"The new declaration, with a doc comment to replace the old one; empty to delete it."`
}

func (*ReplaceDeclaration) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a replaceDeclarationArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) { return p.ReplaceDeclaration(path, a.Name, a.Content) })
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	did := "replaced"
	if strings.TrimSpace(a.Content) == "" {
		did = "deleted"
	}
	return &refactor.Plan{Summary: fmt.Sprintf("%s %s", did, a.Name), Edits: edits}, nil
}
