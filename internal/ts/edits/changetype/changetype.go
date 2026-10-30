// Package changetype is change_type for TypeScript: an edit that changes
// what a type alias stands for.
package changetype

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/tsapi"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// ChangeType changes what a type alias stands for.
type ChangeType struct{}

// New returns change_type.
func New() *ChangeType { return &ChangeType{} }

func (*ChangeType) ChangesBehavior() {}

func (*ChangeType) Name() string { return "change_type" }

func (*ChangeType) Description() string {
	return `Change what a type alias stands for: type Liters = number to type Liters = number | string. An edit: every use takes the new type, so run it in a batch with check "end" together with the changes the uses then need.
Give file, name (the alias) and type (what it is to stand for). An interface or class is refused: change its members with change_field_type.`
}

func (*ChangeType) InputSchema() json.RawMessage {
	return refactor.Schema[changeTypeArgs]()
}

// changeTypeArgs are the arguments of change_type.
type changeTypeArgs struct {
	File string `json:"file" desc:"The file that declares the alias, relative to the root."`
	Name string `json:"name" desc:"The alias's name."`
	Type string `json:"type" desc:"What it is to stand for, as TypeScript valid in that file."`
}

func (*ChangeType) Plan(ws *workspace.Workspace, raw json.RawMessage) (*refactor.Plan, error) {
	var a changeTypeArgs
	if err := refactor.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	path := ws.Abs(a.File)
	if _, err := os.Stat(path); err != nil {
		return nil, refactor.Refusef("%s: no such file", a.File)
	}
	edits, why := ws.Edits(path, func(p *tsapi.Project) ([]tsapi.Edit, string) { return p.ChangeType(path, a.Name, a.Type) })
	if why != "" {
		return nil, refactor.Refusef("%s", why)
	}
	return &refactor.Plan{Summary: fmt.Sprintf("changed %s to %s", a.Name, a.Type), Edits: edits}, nil
}
