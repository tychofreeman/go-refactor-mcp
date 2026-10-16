package m

import "fmt"

type T struct{ n int }

func (t T) Run() {
	fmt.Println(t.n)
}
