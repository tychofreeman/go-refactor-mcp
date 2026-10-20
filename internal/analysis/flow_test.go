package analysis

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestTerminatingList(t *testing.T) {
	src := `package p

func a() int { return 1 }

func b(x int) int {
	if x > 0 {
		return 1
	} else {
		panic("no")
	}
}

func c() int {
outer:
	for {
		for {
			break outer
		}
	}
	return 0
}

func d() int {
	for {
		break
	}
	panic("x")
}

func e(x int) int {
	switch x {
	case 1:
		fallthrough
	case 2:
		return 2
	default:
		panic("x")
	}
}

func f(x int) int {
	switch x {
	case 1:
		return 1
	}
	return 0
}

func g() int {
	for {
	}
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("example.com/p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	for _, d := range file.Decls {
		fd := d.(*ast.FuncDecl)
		list := fd.Body.List
		if !TerminatingList(info, list, nil) {
			t.Errorf("%s: its body does not end in a terminating statement", fd.Name.Name)
		}
		// With its last statement taken as a call, the body ends in none.
		if last := list[len(list)-1]; TerminatingList(info, list, last) {
			t.Errorf("%s: still terminating with its last statement taken as a call", fd.Name.Name)
		}
	}
	// A nested statement taken as a call: one branch of b's if.
	b := file.Decls[1].(*ast.FuncDecl).Body.List
	inner := b[0].(*ast.IfStmt).Else.(*ast.BlockStmt).List[0]
	if TerminatingList(info, b, inner) {
		t.Errorf("b: still terminating with its else branch's panic taken as a call")
	}
	// A list that does not end in one.
	if TerminatingList(info, []ast.Stmt{&ast.EmptyStmt{}}, nil) {
		t.Errorf("an empty statement is terminating")
	}
}
