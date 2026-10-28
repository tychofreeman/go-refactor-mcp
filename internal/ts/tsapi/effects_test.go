package tsapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const moves = `let shared = 0;

function bump(): void {
  shared++;
}

function twice(x: number): number {
  const y = x * 2;
  return y;
}

function count(x: number): number {
  return x > 0 ? count(x - 1) : 0;
}

class Box {
  static make(): number {
    return twice(1);
  }
}

export function assigned(): number {
  let a = 1;
  const b = a + 1;
  a = 2;
  return a + b;
}

export function pure(): number {
  const b = twice(2) + Box.make();
  bump();
  return b;
}

export function read(p: { h: number }): number {
  const b = p.h;
  bump();
  return b;
}

export function leaves(): void {
  bump();
  return;
}

export function loop(xs: number[]): number {
  const b = xs.length;
  for (const x of xs) {
    if (x > 1) {
      break;
    }
  }
  return b;
}

export function captured(): number {
  let a = 1;
  const inc = () => {
    a++;
  };
  const b = a;
  inc();
  return b;
}

export function values(p?: { h: number }): number {
  const b = p!.h;
  const c = [1];
  const d = count(3);
  return b + c.length + d;
}
`

func TestMove(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noEmit": true, "types": []}, "include": ["src"]}`,
		"src/a.ts":      moves,
	}
	for name, text := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Load(filepath.Join(dir, "tsconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	l, why := p.lined(filepath.Join(dir, "src/a.ts"))
	if why != "" {
		t.Fatal(why)
	}
	c, done := p.prog.GetProgram().GetTypeCheckerForFile(context.Background(), l.f)
	defer done()
	a := newEffects(c, l)

	// Each moves statement expr of fn (its value when value) past
	// statements from to to, times as often.
	for _, tc := range []struct {
		fn       string
		expr     int
		value    bool
		from, to int
		times    Times
		want     string
	}{
		{"assigned", 1, true, 2, 3, Once, "a = 2 at a.ts:25 assigns a, and a at a.ts:24 reads it"},
		{"pure", 0, true, 1, 2, Once, ""},
		{"read", 0, true, 1, 2, Once, "the call bump() at a.ts:37 may change state, and p.h at a.ts:36 reads state"},
		{"leaves", 0, false, 1, 2, Once, "the call bump() at a.ts:42 may change state, and return; at a.ts:43 leaves"},
		{"loop", 0, true, 1, 2, Once, ""},
		{"captured", 2, true, 3, 4, Once, "the call inc() at a.ts:62 may change state, and a at a.ts:61 reads state"},
		{"read", 0, true, 0, 0, Several, ""},
		{"read", 0, true, 1, 2, Several, "the call bump() at a.ts:37 may change state, and p.h at a.ts:36 reads state"},
		{"values", 1, true, 0, 0, Shared, "[1] would be evaluated once for several uses, and [1] at a.ts:68 makes a new value each time"},
		{"values", 0, true, 0, 0, Maybe, "p!.h would then not always be evaluated, and p!.h, which reads a property of what may be null or undefined, at a.ts:67 may throw"},
		{"values", 2, true, 0, 0, Maybe, "and the call count(3) at a.ts:69 may change state"},
	} {
		fn, _, _, why := l.function(tc.fn)
		if why != "" {
			t.Fatal(why)
		}
		stmts := fn.Body().Statements()
		expr := stmts[tc.expr]
		if tc.value {
			expr = expr.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Initializer()
		}
		got := a.Check(Move{Expr: expr, Past: stmts[tc.from:tc.to], Times: tc.times})
		if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("%s, statement %d: want %q, got %q", tc.fn, tc.expr, tc.want, got)
		}
	}
}
