package m

import "fmt"

func run() {
	n := 0
	inc := func() { n++ }
	inc()
	fmt.Println(n)
	more(&n, inc)
}

func more(n *int, inc func()) {
	*n += 10
	inc()
	fmt.Println(*n)
}
