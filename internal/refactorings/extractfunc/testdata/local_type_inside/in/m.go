package m

import "fmt"

func run() {
	type pair struct{ a, b int }
	p := pair{1, 2}
	fmt.Println(p.a + p.b)
}
