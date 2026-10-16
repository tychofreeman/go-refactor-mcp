package m

import (
	"fmt"

	"example.com/m/a"
)

func run() {
	v := a.Get()
	fmt.Println(v)
}
