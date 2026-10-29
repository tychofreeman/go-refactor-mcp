package tsapi

import (
	"strconv"
	"strings"

	"github.com/buke/typescript-go-internal/v7/pkg/ast"
)

// early plans how a body that returns early is inlined where its call is
// a statement or a declaration's or assignment's value: each return
// assigns the call's variable instead; the statements after an if that
// returns become its else; any other return also sets a flag that guards
// the statements after it, and in a loop breaks out of the outermost one,
// labelled.
type early struct {
	in       *inliner
	rs       []repl                  // the body's edits
	target   string                  // the variable each return's value goes to, or "" for a call statement
	flag     string                  // the flag's name, once a guard reads it
	used     map[string]bool         // names and labels the function and the caller use
	taken    func(string) bool       // whether a name means something at the call
	exits    map[*ast.Node]string    // each return planned, with the label it breaks out of, if any
	flagged  map[*ast.Node]bool      // the returns that set the flag
	breaks   map[*ast.Node]bool      // returns in a switch, which then break out of it: a case would fall through
	sw       bool                    // planning a switch's case, outside any loop in it
	hoisted  map[*ast.Node]*ast.Node // a last return whose value is assigned before the loop or switch that returns early instead
	params   []repl                  // the parameters' replacements, for a hoisted value
	declInit string                  // the hoisted value, when it initializes the call's variable
	decl     bool                    // the call declares the target
	unit     string                  // the file's indentation step
}

// insert adds text at off to the body's edits, after any text already
// inserted there.
func (e *early) insert(off int, text string) {
	for i, r := range e.rs {
		if r.start == off && r.end == off {
			e.rs[i].text += text
			return
		}
	}
	e.rs = append(e.rs, repl{off, off, text})
}

// fresh returns base, or base with a number, that is not taken.
func (e *early) fresh(base string) string {
	name := base
	for i := 2; e.used[name] || e.taken(name); i++ {
		name = base + strconv.Itoa(i)
	}
	e.used[name] = true
	return name
}

// indentLines indents each line of the body from the one at off to the
// one ending at end by one step.
func (e *early) indentLines(off, end int) {
	fl := e.in.fl
	for n := fl.line(off); n <= fl.line(end); n++ {
		e.insert(fl.start(n), e.unit)
	}
}

// plan plans the returns in list, as Go's inline_function does (see its
// exits): tail says whether falling off list ends the body, loop is the
// label of the loop list is in, or "". It reports whether a return in list
// sets the flag, or why the body cannot be planned.
func (e *early) plan(list []*ast.Node, tail bool, loop string) (flagged bool, why string) {
	fl := e.in.fl
	for i, s := range list {
		if !hasReturn(s) {
			continue
		}
		last := i == len(list)-1
		rest := list[i+1:]
		var f bool
		switch {
		case ast.IsReturnStatement(s):
			if !last {
				return false, "it has statements after a return (" + fl.where(s) + "), which never run"
			}
			e.exits[s] = loop
			if e.sw && loop == "" {
				e.breaks[s] = true
			}
			if tail {
				return flagged, ""
			}
			e.flagged[s] = true
			return true, ""
		case ast.IsIfStatement(s):
			is := s.AsIfStatement()
			if !last && tail && loop == "" && is.ElseStatement == nil && returnsAtEnd(is.ThenStatement) {
				// The statements after the if run only when it does not
				// return: they become its else branch.
				if _, why := e.plan(branch(is.ThenStatement), true, loop); why != "" {
					return false, why
				}
				e.insert(is.ThenStatement.End(), " else {")
				e.indentLines(fl.pos(rest[0]), rest[len(rest)-1].End())
				e.insert(rest[len(rest)-1].End(), "\n"+fl.indent(fl.line(fl.pos(s)))+"}")
				g, why := e.plan(rest, true, loop)
				return flagged || g, why
			}
			for b := s; b != nil; {
				var body []*ast.Node
				if ast.IsIfStatement(b) {
					body, b = branch(b.AsIfStatement().ThenStatement), b.AsIfStatement().ElseStatement
				} else {
					body, b = branch(b), nil
				}
				g, why := e.plan(body, last && tail, loop)
				if why != "" {
					return false, why
				}
				f = f || g
			}
		case ast.IsSwitchStatement(s):
			sw := e.sw
			e.sw = true
			for _, c := range s.AsSwitchStatement().CaseBlock.AsCaseBlock().Clauses.Nodes {
				g, why := e.plan(c.Statements(), last && tail, loop)
				e.sw = true
				if why != "" {
					return false, why
				}
				f = f || g
			}
			e.sw = sw
		case ast.IsBlock(s):
			var why string
			if f, why = e.plan(s.Statements(), last && tail, loop); why != "" {
				return false, why
			}
		case ast.IsLabeledStatement(s) || ast.IsForStatement(s) || ast.IsForInOrOfStatement(s) || ast.IsWhileStatement(s) || ast.IsDoStatement(s):
			label, body := loop, s
			if ast.IsLabeledStatement(s) {
				body = s.AsLabeledStatement().Statement
			}
			if !(ast.IsForStatement(body) || ast.IsForInOrOfStatement(body) || ast.IsWhileStatement(body) || ast.IsDoStatement(body)) {
				return false, "it returns inside a labelled statement that is not a loop (" + fl.where(s) + ")"
			}
			if label == "" { // the outermost loop: a return breaks out of all of them
				if ast.IsLabeledStatement(s) {
					label = s.AsLabeledStatement().Label.Text()
				} else {
					label = e.fresh("loop")
					e.insert(fl.pos(s), label+": ")
				}
			}
			var why string
			sw := e.sw
			e.sw = false
			f, why = e.plan(branch(body.Statement()), false, label)
			if e.sw = sw; why != "" {
				return false, why
			}
		default:
			return false, "it returns inside a statement other than an if, switch, block or loop (" + fl.where(s) + ")"
		}
		flagged = flagged || f
		if f && tail && loop == "" && e.hoistable(s, rest) {
			// The value the body returns otherwise is assigned before s:
			// each return in s overwrites it and leaves s, and nothing else
			// runs after it.
			e.hoisted[rest[0]] = s
			return flagged, ""
		}
		if f && !last && loop == "" {
			// The statements after s run only if nothing in it returned.
			if e.flag == "" {
				e.flag = e.fresh("returned")
			}
			at := fl.start(fl.line(fl.pos(rest[0])))
			ind := fl.indent(fl.line(fl.pos(rest[0])))
			e.insert(at, ind+"if (!"+e.flag+") {\n")
			e.indentLines(fl.pos(rest[0]), rest[len(rest)-1].End())
			e.insert(rest[len(rest)-1].End(), "\n"+ind+"}")
			g, why := e.plan(rest, tail, loop)
			return flagged || g, why
		}
	}
	return flagged, ""
}

// hoistable reports whether rest, which follows s, is only a return of a
// value the call assigns, which can be evaluated before s instead: it
// does nothing and cannot throw, so evaluating it also when s returns
// changes nothing, and s changes nothing it reads.
func (e *early) hoistable(s *ast.Node, rest []*ast.Node) bool {
	if len(rest) != 1 || !ast.IsReturnStatement(rest[0]) || rest[0].Expression() == nil || e.target == "" {
		return false
	}
	return e.in.ef.Check(Move{Expr: rest[0].Expression(), Past: []*ast.Node{s}, Times: Maybe}) == ""
}

// finish adds the edits of the returns: each assigns its value to the
// target, sets the flag and breaks out of its loop as planned.
func (e *early) finish() {
	fl, semi := e.in.fl, e.in.fl.semi()
	for r, s := range e.hoisted {
		x := r.Expression()
		value := fl.substitute(fl.pos(x), x.End(), e.params)
		if a, b, ok := fl.lineRange(r); ok {
			e.rs = append(e.rs, repl{a, b, ""})
		} else {
			e.rs = append(e.rs, repl{fl.pos(r), r.End(), ""})
		}
		if e.decl && s.Parent == e.in.fn.Body() {
			e.declInit = value
			continue
		}
		e.insert(fl.start(fl.line(fl.pos(s))), fl.indent(fl.line(fl.pos(s)))+e.target+" = "+value+semi+"\n")
	}
	for r, label := range e.exits {
		var then []string
		if e.flagged[r] && e.flag != "" {
			then = append(then, e.flag+" = true"+semi)
		}
		switch {
		case e.flagged[r] && label != "":
			then = append(then, "break "+label+semi)
		case e.breaks[r]:
			then = append(then, "break"+semi)
		}
		ind := fl.indent(fl.line(fl.pos(r)))
		x := r.Expression()
		switch {
		case x != nil && e.target != "":
			e.rs = append(e.rs, repl{fl.pos(r), fl.pos(r) + len("return"), e.target + " ="})
		case x != nil && !e.in.ef.Of(x).CanDrop():
			e.rs = append(e.rs, repl{fl.pos(r), fl.pos(x), ""})
		case len(then) > 0:
			e.rs = append(e.rs, repl{fl.pos(r), r.End(), strings.Join(then, "\n"+ind)})
			continue
		default:
			if a, b, ok := fl.lineRange(r); ok {
				e.rs = append(e.rs, repl{a, b, ""})
			} else {
				e.rs = append(e.rs, repl{fl.pos(r), r.End(), ""})
			}
			continue
		}
		if len(then) > 0 {
			e.insert(r.End(), "\n"+ind+strings.Join(then, "\n"+ind))
		}
	}
}

// branch returns the statements of s, a block or one statement.
func branch(s *ast.Node) []*ast.Node {
	if s == nil {
		return nil
	}
	if ast.IsBlock(s) {
		return s.Statements()
	}
	return []*ast.Node{s}
}

// returnsAtEnd reports whether s always ends in a return: it is one, or
// it is a block or an if whose branches all end in one.
func returnsAtEnd(s *ast.Node) bool {
	switch {
	case s == nil:
		return false
	case ast.IsReturnStatement(s):
		return true
	case ast.IsBlock(s):
		list := s.Statements()
		return len(list) > 0 && returnsAtEnd(list[len(list)-1])
	case ast.IsIfStatement(s):
		is := s.AsIfStatement()
		return returnsAtEnd(is.ThenStatement) && returnsAtEnd(is.ElseStatement)
	}
	return false
}

// hasReturn reports whether s holds a return of the function it is in.
func hasReturn(s *ast.Node) bool {
	found := false
	var visit func(n *ast.Node) bool
	visit = func(n *ast.Node) bool {
		switch {
		case found || ast.IsFunctionLike(n) || ast.IsClassLike(n):
			return false
		case ast.IsReturnStatement(n):
			found = true
			return false
		}
		n.ForEachChild(visit)
		return false
	}
	visit(s)
	return found
}
