package m

import "fmt"

func run() {
	n, err := 0, error(nil)
	p := &n
	if true {
		fmt.Println(n)
	}
	m := step(&n)
	fmt.Println(m, n, *p, err)
}

func step(n *int) int {
	var m int
	m, *n = 3, 4
	return m
}
