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
type ChangeType struct{ refactor.Named[changeTypeArgs] }

// New returns change_type.
func New() *ChangeType { return &ChangeType{Named: refactor.Called[changeTypeArgs]("change_type")} }

// Route names the tool the arguments ask for (refactor.ChangeTypeRoute).
func (*ChangeType) Route(args json.RawMessage) (string, error) { return refactor.ChangeTypeRoute(args) }

// Covers names the tools change_type stands for.
func (*ChangeType) Covers() []string { return refactor.ChangeTypeCovers }

func (*ChangeType) ChangesBehavior() {}

func (*ChangeType) Description() string {
	return `Change a declared type; what you give says which. Types are erased, so no value changes, but the uses stay as they are: each is an edit, so run it in a batch with check "end" together with the changes the uses then need.
- name Type.property: a property's type, of a class, an interface or an object type: with type Centimeters, height: number becomes height: Centimeters. values, narrowing, maps each old constant to the new type's value: {"fern": "FERN"} turns species: "fern" into species: FERN.
- name (a function, or Class.method) and parameter: the parameter's type: water(bed: string, liters: number) becomes water(bed: string, liters: Liters); a method changes with its family.
- name, an alias: what it stands for: type Liters = number to type Liters = number | string.
Refuses, saying why: a method or accessor as a property, overload signatures, a destructured parameter, an interface or class named alone.`
}

// changeTypeArgs are the arguments of change_type.
type changeTypeArgs struct {
	File      string            `json:"file" desc:"The file that declares the alias, the type or the function, relative to the root."`
	Name      string            `json:"name" desc:"The alias's name; Type.property for a property; the function (or Class.method) with parameter."`
	Type      string            `json:"type" desc:"What it is to stand for, as TypeScript valid in that file."`
	Parameter string            `json:"parameter,omitempty" desc:"For a parameter: its name; name is then the function."`
	Values    map[string]string `json:"values,omitempty" desc:"For a property, narrowing: for each old constant (\"fern\", or as written), the new type's value, e.g. {\"fern\": \"FERN\"}."`
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
