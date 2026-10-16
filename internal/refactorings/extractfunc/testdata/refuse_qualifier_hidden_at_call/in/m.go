package m

import (
	"fmt"

	"example.com/m/a"
	"example.com/m/b"
)

var _ b.T

func run() {
	count := 0
	b := 1
	count++
	t := a.Get()
	fmt.Println(count, t, b)
}
