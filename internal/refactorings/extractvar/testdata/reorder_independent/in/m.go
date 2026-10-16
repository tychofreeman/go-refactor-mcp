package m

import "fmt"

var counter int

func next() int {
	counter++
	return counter
}

func Show(a, b int) {
	fmt.Println(next(), a+b)
}
