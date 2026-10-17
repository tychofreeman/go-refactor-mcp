package m

import (
	"fmt"
	"sort"
)

func collect(in []int, extra []int) {
	var out []int
	fmt.Println(extra)
	out = addEvens(in, out)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	sort.Slice(extra, func(i, j int) bool { return extra[i] < extra[j] })
	fmt.Println(out, extra)
}

func addEvens(in []int, out []int) []int {
	for _, n := range in {
		out = append(out, n*2)
	}
	return out
}
