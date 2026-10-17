package m

import "fmt"

func run(s string) {
	if n := len(s); n > 3 {
		fmt.Println(n)
	}
}
