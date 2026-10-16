package m

import (
	"fmt"

	"example.com/m/a"
)

func run(b int) {
	var t = a.Get()
	t = a.Get()
	fmt.Println(b, t)
	fmt.Println(t)
}
