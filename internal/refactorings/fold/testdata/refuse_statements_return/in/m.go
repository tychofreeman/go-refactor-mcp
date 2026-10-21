package m

import "fmt"

func check(n int) {
	if n < 0 {
		return
	}
	fmt.Println(n)
}
