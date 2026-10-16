package m

import (
	"fmt"

	"example.com/m/a"
)

var b = 3

func run() {
	v := a.Get()
	fmt.Println(v.N, b)
}
