package m

import "fmt"

func run() {
	n := 1
	inc := func() { n++ }
	inc()
	fmt.Println(n)
}
