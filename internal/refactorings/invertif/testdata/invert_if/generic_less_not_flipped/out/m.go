package m

import (
	"cmp"
	"fmt"
)

func run[T cmp.Ordered](a, b T) {
	if !(a < b) {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
