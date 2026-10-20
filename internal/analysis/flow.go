package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
)

// EndsFlow reports whether control cannot reach the end of list: its last
// statement is a terminating statement (as the Go spec defines it: return,
// goto, panic, an infinite loop, ...) or a break or continue. Calls that
// never return without the spec saying so (os.Exit, log.Fatal) do not
// count, so the answer is conservative.
func EndsFlow(info *types.Info, list []ast.Stmt) bool {
	for i := len(list) - 1; i >= 0; i-- {
		switch s := list[i].(type) {
		case *ast.EmptyStmt:
			continue
		case *ast.BranchStmt:
			if s.Tok == token.BREAK || s.Tok == token.CONTINUE || s.Tok == token.GOTO {
				return true
			}
		}
		return Terminating(info, list[i])
	}
	return false
}

// Terminating reports whether s is a terminating statement as defined by
// the Go spec.
func Terminating(info *types.Info, s ast.Stmt) bool {
	return terminating(info, s, nil, nil)
}

// TerminatingList reports whether list ends in a terminating statement.
// When call is not nil, it counts as replaced by a call of a new
// function, which is not one: the last statement extract_function moves.
func TerminatingList(info *types.Info, list []ast.Stmt, call ast.Stmt) bool {
	for i := len(list) - 1; i >= 0; i-- {
		switch s := list[i]; {
		case s == call:
			return false
		case isEmpty(s):
			continue
		default:
			return terminating(info, s, nil, call)
		}
	}
	return false
}

func isEmpty(s ast.Stmt) bool {
	_, ok := s.(*ast.EmptyStmt)
	return ok
}

// terminating is Terminating for s, labeled with label (or nil), with
// call counted as TerminatingList does.
func terminating(info *types.Info, s ast.Stmt, label types.Object, call ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.GOTO
	case *ast.ExprStmt:
		if c, ok := ast.Unparen(s.X).(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(c.Fun).(*ast.Ident); ok {
				return info.Uses[id] == types.Universe.Lookup("panic")
			}
		}
	case *ast.BlockStmt:
		return TerminatingList(info, s.List, call)
	case *ast.IfStmt:
		return s.Else != nil && terminating(info, s.Body, nil, call) && terminating(info, s.Else, nil, call)
	case *ast.ForStmt:
		return s.Cond == nil && !breaksOut(info, s, label)
	case *ast.LabeledStmt:
		return terminating(info, s.Stmt, info.Defs[s.Label], call)
	case *ast.SwitchStmt:
		return terminatingClauses(info, s, s.Body, label, false, call)
	case *ast.TypeSwitchStmt:
		return terminatingClauses(info, s, s.Body, label, false, call)
	case *ast.SelectStmt:
		return terminatingClauses(info, s, s.Body, label, true, call)
	}
	return false
}

// terminatingClauses applies the spec's rule for switch and select: no
// break refers to the statement, a switch has a default case, and every
// clause ends in a terminating statement (or, in a switch, fallthrough).
func terminatingClauses(info *types.Info, stmt ast.Stmt, body *ast.BlockStmt, label types.Object, isSelect bool, call ast.Stmt) bool {
	if breaksOut(info, stmt, label) {
		return false
	}
	hasDefault := false
	for _, c := range body.List {
		var list []ast.Stmt
		switch c := c.(type) {
		case *ast.CaseClause:
			list, hasDefault = c.Body, hasDefault || c.List == nil
		case *ast.CommClause:
			list = c.Body
		}
		if n := len(list); n > 0 {
			if b, ok := list[n-1].(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
				continue
			}
		}
		if !TerminatingList(info, list, call) {
			return false
		}
	}
	return isSelect || hasDefault
}

// breaksOut reports whether a break inside stmt refers to it: unlabeled
// and not nested in another for, switch or select, or labeled with label.
func breaksOut(info *types.Info, stmt ast.Stmt, label types.Object) bool {
	found := false
	var walk func(n ast.Node, nested bool)
	walk = func(n ast.Node, nested bool) {
		ast.Inspect(n, func(c ast.Node) bool {
			if found || c == nil {
				return false
			}
			switch c := c.(type) {
			case *ast.FuncLit:
				return false
			case *ast.BranchStmt:
				if c.Tok == token.BREAK && (c.Label == nil && !nested || c.Label != nil && label != nil && info.Uses[c.Label] == label) {
					found = true
				}
			case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				if c != n {
					walk(c, true)
					return false
				}
			}
			return true
		})
	}
	walk(stmt, false)
	return found
}
