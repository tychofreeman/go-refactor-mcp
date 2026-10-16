package m

import "fmt"

func run(bad bool) {
	if bad {
		fmt.Println("bad")
		return
	}
	fmt.Println("ok")
}
