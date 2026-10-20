package m

import "fmt"

// Run prints.
func (t T) Run(n int) int {
	fmt.Println("a")
	if n > 0 {
		return n
	}
	return 0
}

type T struct{}

func (t T) M() { fmt.Println("m") }

func F(
	n int,
) int {
	fmt.Println("a")
	if n > 0 {
		return n
	}
	return 0
}

func Empty() {
}
