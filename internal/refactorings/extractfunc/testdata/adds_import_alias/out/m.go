package m

import (
	"fmt"

	"example.com/m/a"
	b2 "example.com/m/b"
)

var b = 3

func run() {
	v := a.Get()
	show(v)
}

func show(v b2.T) {
	fmt.Println(v.N, b)
}
