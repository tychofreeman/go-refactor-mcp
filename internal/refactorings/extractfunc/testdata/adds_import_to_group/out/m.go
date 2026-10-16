package m

import (
	"fmt"

	"example.com/m/a"
	"example.com/m/b"
)

func run() {
	v := a.Get()
	step(v)
}

func step(v b.T) {
	fmt.Println(v)
}
