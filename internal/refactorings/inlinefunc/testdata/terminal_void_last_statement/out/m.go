package m

import "fmt"

func run(n int) {
	fmt.Println("start")
	if n < 0 {
		fmt.Println("negative")
		return
	}
	fmt.Println("ok")
}
