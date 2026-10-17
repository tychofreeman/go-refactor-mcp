package m

import "fmt"

func run(s string) {
	if n := length(s); n > 3 {
		fmt.Println(n)
	}
}

func length(s string) int {
	return len(s)
}
