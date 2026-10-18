package m

import "fmt"

func squares(n int) []int {
	out := make([]int, 0, n)
	for i := range n {
		out = append(out, i*i)
	}
	return out
}

func F(n int) {
	sq := squares(n)
	fmt.Println(n)
	fmt.Println(sq)
}
