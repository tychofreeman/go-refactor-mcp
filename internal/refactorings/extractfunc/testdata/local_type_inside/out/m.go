package m

import "fmt"

func run() {
	sumPair()
}

func sumPair() {
	type pair struct{ a, b int }
	p := pair{1, 2}
	fmt.Println(p.a + p.b)
}
