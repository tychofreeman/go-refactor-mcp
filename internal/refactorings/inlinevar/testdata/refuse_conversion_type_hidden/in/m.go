package m

import "fmt"

type Flag bool

func Use(a, b int) {
	var f Flag = a == b
	type Flag int
	_ = Flag(0)
	fmt.Println(f)
}
