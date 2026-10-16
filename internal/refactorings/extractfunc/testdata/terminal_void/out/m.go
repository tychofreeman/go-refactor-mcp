package m

import "fmt"

func run(bad bool) {
	if bad {
		complain()
		return
	}
	fmt.Println("ok")
}

func complain() {
	fmt.Println("bad")
	return
}
