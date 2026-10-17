package m

import "fmt"

func run(m map[string]bool, k string) {
	if !m[k] {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
