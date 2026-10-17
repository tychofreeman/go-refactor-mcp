package m

import "fmt"

func run(s, t string) {
	if s > t {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
