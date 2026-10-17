package m

import "fmt"

type T struct{ n int }

func (t *T) Run() func() {
	return func() {
		fmt.Println(t.n)
	}
}
