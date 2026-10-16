package m

import "fmt"

type T struct{}

func (T) Run() {
	fmt.Println("a")
	fmt.Println("b")
}
