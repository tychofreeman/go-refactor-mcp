package m

import "fmt"

func run(s, t string) {
	if s <= t {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
