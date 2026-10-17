package m

import "fmt"

type T struct{ n int }

func (t *T) Run() func() {
	return func() {
		show(t)
	}
}

func show(t *T) {
	fmt.Println(t.n)
}
