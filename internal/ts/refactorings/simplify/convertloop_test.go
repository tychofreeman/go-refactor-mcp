package simplify

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const garden = `export function water(p: unknown): void {}

`

func TestConvertLoop(t *testing.T) {
	r := NewConvertLoop()
	check(t, []change{
		{name: "index to for...of", r: r, args: `{"line": 4, "to": "for_each"}`,
			src: garden + `export function all(plants: string[]): void {
  for (let i = 0; i < plants.length; i++) {
    water(plants[i]);
  }
}
`, out: garden + `export function all(plants: string[]): void {
  for (const plant of plants) {
    water(plant);
  }
}
`},
		{name: "index to for...of naming the element", r: r, args: `{"line": 4, "to": "for_each"}`,
			src: garden + `export function all(beds: number[][]): void {
  for (let i = 0; i < beds.length; i += 1) {
    const bed = beds[i];
    water(bed.length);
  }
}
`, out: garden + `export function all(beds: number[][]): void {
  for (const bed of beds) {
    water(bed.length);
  }
}
`},
		{name: "index read otherwise", r: r, args: `{"line": 4, "to": "for_each"}`,
			src: garden + `export function all(plants: string[]): void {
  for (let i = 0; i < plants.length; i++) {
    water(i + plants[i]);
  }
}
`, refused: "reads i other than as plants[i]"},
		{name: "index over a changed array", r: r, args: `{"line": 4, "to": "for_each"}`,
			src: garden + `export function all(plants: string[]): void {
  for (let i = 0; i < plants.length; i++) {
    if (plants[i] === "") plants.splice(i, 1);
  }
}
`, refused: "changes plants"},
		{name: "for...of to index", r: r, args: `{"line": 4, "to": "for"}`,
			src: garden + `export function all(plants: string[]): void {
  for (const plant of plants) {
    water(plant);
  }
}
`, out: garden + `export function all(plants: string[]): void {
  for (let i = 0; i < plants.length; i++) {
    const plant = plants[i];
    water(plant);
  }
}
`},
		{name: "for...of over a set", r: r, args: `{"line": 4, "to": "for"}`,
			src: garden + `export function all(plants: Set<string>): void {
  for (const plant of plants) {
    water(plant);
  }
}
`, refused: "not an array"},
		{name: "for to while", r: r, args: `{"line": 4, "to": "while"}`,
			src: garden + `export function odd(n: number): void {
  outer: for (let i = 0; i < n; i++) {
    if (i % 2 === 0) continue;
    for (let j = 0; j < i; j++) {
      if (j === 1) continue;
      if (j === 2) continue outer;
    }
    water(i);
  }
}
`, out: garden + `export function odd(n: number): void {
  let i = 0;
  outer: while (i < n) {
    if (i % 2 === 0) {
      i++;
      continue;
    }
    for (let j = 0; j < i; j++) {
      if (j === 1) continue;
      if (j === 2) {
        i++;
        continue outer;
      }
    }
    water(i);
    i++;
  }
}
`},
		{name: "for to while in a block", r: r, args: `{"line": 4, "to": "while"}`,
			src: garden + `export function twice(): void {
  for (let i = 0; i < 3; i++) {
    water(i);
  }
  for (let i = 0; i < 2; i++) {
    water(-i);
  }
}
`, out: garden + `export function twice(): void {
  {
    let i = 0;
    while (i < 3) {
      water(i);
      i++;
    }
  }
  for (let i = 0; i < 2; i++) {
    water(-i);
  }
}
`},
		{name: "for to while with a captured let", r: r, args: `{"line": 5, "to": "while"}`,
			src: garden + `export function later(): (() => number)[] {
  const fs: (() => number)[] = [];
  for (let i = 0; i < 3; i++) {
    fs.push(() => i);
  }
  return fs;
}
`, refused: "captures i"},
		{name: "while to for", r: r, args: `{"line": 5, "to": "for"}`,
			src: garden + `export function count(): void {
  let i = 0;
  while (i < 3) {
    water(i);
    i++;
  }
}
`, out: garden + `export function count(): void {
  for (let i = 0; i < 3; i++) {
    water(i);
  }
}
`},
		{name: "do to while", r: r, args: `{"line": 5, "to": "while"}`,
			src: garden + `export function count(): void {
  let i = 0;
  do {
    i++;
  } while (i < 3);
  water(i);
}
`, out: garden + `export function count(): void {
  let i = 0;
  while (true) {
    i++;
    if (!(i < 3)) break;
  }
  water(i);
}
`},
		{name: "do with a continue", r: r, args: `{"line": 5, "to": "while"}`,
			src: garden + `export function count(): void {
  let i = 0;
  do {
    i++;
    if (i === 2) continue;
    water(i);
  } while (i < 3);
}
`, refused: "goes to its condition"},
		{name: "while to do...while", r: r, args: `{"line": 4, "to": "do_while"}`,
			src: garden + `export function count(n: number, i: number): void {
  while (i < n) {
    water(i);
    i++;
  }
}
`, out: garden + `export function count(n: number, i: number): void {
  if (i < n) {
    do {
      water(i);
      i++;
    } while (i < n);
  }
}
`},
		{name: "while to do...while reading a property", r: r, args: `{"line": 4, "to": "do_while"}`,
			src: garden + `export function drain(bed: { wet: boolean }): void {
  while (bed.wet) {
    water(bed);
  }
}
`, refused: "does more than read a local"},
	})
}

// TestConvertLoopProbes runs each program with node before and after a
// conversion: it must print the same.
func TestConvertLoopProbes(t *testing.T) {
	if testing.Short() {
		t.Skip("runs node")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	r := NewConvertLoop()
	for _, tc := range []struct{ name, args, src string }{
		{"for to while with continue", `{"line": 3, "to": "while"}`, `let out = "";
let ticks = 0;
outer: for (let i = 0, j = 0; i < 12; i = (ticks++, i + 1), j += 2) {
  if (i % 3 === 0) continue;
  for (let k = 0; k < 2; k++) {
    if (k === 1 && i === 4) continue outer;
  }
  if (i === 10) break;
  out += i + ":" + j + " ";
}
console.log(out, ticks);
`},
		{"do to while with break", `{"line": 4, "to": "while"}`, `let i = 10;
let checks = 0;
const more = (n: number) => { checks++; return n < 5; };
do {
  i++;
  if (i === 13) break;
  console.log(i);
} while (more(i) || i < 12);
console.log(i, checks);
`},
		{"index to for...of", `{"line": 3, "to": "for_each"}`, `const words = ["a", "", "b", "stop", "c"];
let out = "";
outer: for (let i = 0; i < words.length; i++) {
  for (let k = 0; k < 2; k++) {
    if (words[i] === "") continue outer;
    if (words[i] === "stop") break outer;
    out += k + words[i];
  }
}
console.log(out);
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, dir := load(t, map[string]string{"src/g.ts": tc.src})
			before := runNode(t, dir)
			res := run(t, ws, r, `{"file": "src/g.ts", `+tc.args[1:])
			if !res.Applied {
				t.Fatalf("%s", res)
			}
			if after := runNode(t, dir); after != before {
				t.Errorf("printed\n%s\nbefore, and\n%s\nafter:\n%s", before, after, res)
			}
		})
	}
}

func runNode(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, "src/g.ts"))
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "g.mts")
	if err := os.WriteFile(f, src, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", "--experimental-strip-types", "--no-warnings", f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return string(out)
}
