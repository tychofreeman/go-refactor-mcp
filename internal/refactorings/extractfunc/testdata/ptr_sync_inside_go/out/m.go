package m

import (
	"fmt"
	"sort"
)

func run(done chan bool) {
	xs := []int{3, 1, 2}
	go func() {
		sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
		done <- true
	}()
	grow(&xs)
	<-done
	fmt.Println(xs)
}

func grow(xs *[]int) {
	*xs = append(*xs, 4)
}
