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
	fmt.Println(n)
	sq := squares(n)
	fmt.Println(sq)
}
