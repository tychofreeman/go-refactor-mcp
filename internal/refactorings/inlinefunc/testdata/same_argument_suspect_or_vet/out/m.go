package m

import "fmt"

func run(v int) {
	b := v
	fmt.Println("checking")
	ok := v != 1 || b != 2
	fmt.Println(ok)
}
