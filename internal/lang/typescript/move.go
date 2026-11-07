package typescript

import (
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/ts/workspace"
)

// moveDescription is TypeScript's move: move_declaration, move_method and
// move_file, published as one.
const moveDescription = `Move a declaration, a method or a file, and update what refers to it; what you give says which:
- name and to_file: a top-level declaration (function, class, interface, type, enum or variable statement, with its comments) to that file, created if needed: moving water from src/plant.ts to src/water.ts exports it if anything outside uses it and changes import { water } from "./plant" to "./water" in every file. also names more declarations to move with it; with_dependencies moves what it uses.
- name Class.method and to, a class that extends its class: with class Bed extends Soil, name Soil.acid and to Bed put acid() in Bed, when every use reaches it through a Bed.
- file and to, without name: a file or directory, updating the imports of it and its own (src/plant.ts to src/garden/plant.ts); fixtures and data move byte for byte; copy keeps the original; delete deletes it.`

// newMove returns TypeScript's move.
func newMove() refactor.RefactoringOf[*workspace.Workspace] {
	return refactor.NewMove[*workspace.Workspace](moveDescription)
}
