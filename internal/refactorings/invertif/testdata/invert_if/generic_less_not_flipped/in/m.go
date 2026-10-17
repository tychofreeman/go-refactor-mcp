package m

import (
	"cmp"
	"fmt"
)

func run[T cmp.Ordered](a, b T) {
	if a < b {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
