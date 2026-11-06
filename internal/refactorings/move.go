package refactorings

import (
	"github.com/tychofreeman/go-refactor-mcp/internal/refactor"
	"github.com/tychofreeman/go-refactor-mcp/internal/workspace"
)

// moveDescription is Go's move: move_declaration, move_method and
// move_file, published as one.
const moveDescription = `Move a declaration, a method or a file, and update what refers to it; what you give says which:
- name and to_file: a top-level declaration (function, type, variable or constant, with its doc comment) to that file, created if needed; in another directory, to that package: moving defaultHost from notify/notify.go to config/config.go with new_name DefaultHost turns defaultHost() into config.DefaultHost(). A type takes its methods along. also names more declarations to move with it; with_dependencies moves what it uses.
- name Type.Method and to, a struct that embeds the receiver's type: with type Bed struct { Soil }, name Soil.acid and to Bed make func (s *Bed) acid(), and b.Soil.acid() becomes b.acid().
- file and to, without name: a file or directory, byte for byte (fixtures, docs, data: testdata/old_case to testdata/new_case); copy keeps the original; delete deletes it (a Go file whose declarations are still used is refused). Go code moves by declaration.
Refuses, saying why: between a test file and a non-test file; code that uses declarations left behind (move them too, in also); an import cycle; a call no value of the new type reaches; a destination that exists.`

// newMove returns Go's move.
func newMove() refactor.Refactoring {
	return refactor.NewMove[*workspace.Workspace](moveDescription)
}
