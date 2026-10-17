package m

import "fmt"

func run(xs []int) {
	for _, x := range xs {
		if x > 0 {
			show(x)
		}
	}
}

func show(v int) {
	fmt.Println("value")
	fmt.Println(v)
}
