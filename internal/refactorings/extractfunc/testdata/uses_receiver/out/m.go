package m

import "fmt"

type T struct{ n int }

func (t T) Run() {
	t.step()
}

func (t T) step() {
	fmt.Println(t.n)
}
