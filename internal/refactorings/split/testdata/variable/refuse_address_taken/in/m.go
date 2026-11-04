package m

import "fmt"

func Water() {
	n := 1
	p := &n
	fmt.Println(*p)
	n = 3
	fmt.Println(n, *p)
}
