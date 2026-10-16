package m

import "fmt"

func run() {
	n := 1
	f := func() {
		fmt.Println(n)
	}
	f()
}
