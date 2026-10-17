package m

import "fmt"

func run() {
	xs := []int{1}
	p := &xs
	grow(&xs)
	fmt.Println(len(*p), xs[1:])
}

func grow(xs *[]int) {
	(*xs)[0] = 5
	*xs = append(*xs, 2)
}
