package m

import "fmt"

func run(ok bool) {
	if ok {
		// The common case.
		fmt.Println("then") // trailing
	} else {
		// Rare.
		fmt.Println("else")
	}
}
