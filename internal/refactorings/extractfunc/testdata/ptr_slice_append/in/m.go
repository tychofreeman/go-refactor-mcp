package m

import "fmt"

func run() {
	xs := []int{1}
	p := &xs
	xs[0] = 5
	xs = append(xs, 2)
	fmt.Println(len(*p), xs[1:])
}
