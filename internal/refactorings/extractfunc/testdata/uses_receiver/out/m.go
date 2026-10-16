package m

import "fmt"

type T struct{ n int }

func (t T) Run() {
	step(t)
}

func step(t T) {
	fmt.Println(t.n)
}
