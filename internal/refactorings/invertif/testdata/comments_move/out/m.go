package m

import "fmt"

func run(ok bool) {
	if !ok {
		// Rare.
		fmt.Println("else")
	} else {
		// The common case.
		fmt.Println("then") // trailing
	}
}
