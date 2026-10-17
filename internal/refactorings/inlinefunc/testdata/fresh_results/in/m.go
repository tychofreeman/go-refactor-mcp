package m

import "fmt"

func run() {
	a, b := pair()
	fmt.Println(a, b)
}

func pair() (int, int) {
	a := 1
	b := a + 1
	return a, b
}
