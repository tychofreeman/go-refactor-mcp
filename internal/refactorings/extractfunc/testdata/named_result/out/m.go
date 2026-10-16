package m

import "fmt"

func run() (n int) {
	n = setN()
	return
}

func setN() int {
	var n int
	n = 3
	fmt.Println(n)
	return n
}
