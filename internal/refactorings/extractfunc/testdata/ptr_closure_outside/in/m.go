package m

import "fmt"

func run() {
	n := 0
	inc := func() { n++ }
	inc()
	fmt.Println(n)
	n += 10
	inc()
	fmt.Println(n)
}
