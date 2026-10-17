package m

import "fmt"

func run(n int) {
	check(n)
	fmt.Println("after")
}

func check(n int) {
	if n < 0 {
		return
	}
	fmt.Println(n)
}
