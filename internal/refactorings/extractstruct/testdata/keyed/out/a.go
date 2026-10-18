package m

import "go/ast"

type source struct {
	// path is absolute.
	path string
	file *ast.File
	src  []byte `json:"-"` // the file's contents
}

// mover holds what one move needs.
type mover struct {
	name string
	source
	n int
}

func newMover(name, p string, f *ast.File) *mover {
	return &mover{name: name, source: source{path: p, file: f}, n: 1}
}

func (m *mover) size() int { return len(m.src) + len(m.path) + m.n }

func multi(p string) mover {
	return mover{
		name:   "x",
		source: source{path: p, src: nil},
	}
}
