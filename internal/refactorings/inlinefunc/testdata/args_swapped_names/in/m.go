package m

import "fmt"

func run(a, b int) {
	diff(b, a)
}

func diff(a, b int) {
	fmt.Println(a - b)
}
