package m

import "fmt"

func codes(s string) []int {
	out := make([]int, 0, len(s))
	for _, r := range s {
		out = append(out, int(r))
	}
	return out
}

func F(s string) {
	fmt.Println(s)
	cs := codes(s)
	fmt.Println(cs)
}
