package m

import "fmt"

func run() {
	n, err := 0, error(nil)
	p := &n
	if true {
		fmt.Println(n)
	}
	m, n := 3, 4
	fmt.Println(m, n, *p, err)
}
