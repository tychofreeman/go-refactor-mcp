package m

import "fmt"

func run(n int) {
	fmt.Println("start")
	check(n)
}

func check(n int) {
	if n < 0 {
		fmt.Println("negative")
		return
	}
	fmt.Println("ok")
}
