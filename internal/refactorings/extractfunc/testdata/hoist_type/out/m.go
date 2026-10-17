package m

import "fmt"

type pair struct{ a, b int }

func run() {
	step()
}

func step() {
	fmt.Println(pair{1, 2})
}
