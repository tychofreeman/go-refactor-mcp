package m

import "fmt"

func Water(dry bool) {
	n := 1
	fmt.Println(n)
	if dry {
		n = 3
	}
	fmt.Println(n)
}
