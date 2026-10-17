package m

import "fmt"

func run() {
	const (
		a = iota
		b
	)
	fmt.Println(a, b)
}
