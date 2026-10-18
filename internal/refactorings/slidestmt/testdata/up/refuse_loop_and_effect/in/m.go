package m

import "fmt"

func F(n int) int {
	i := 0
	for i < n {
		i++
	}
	fmt.Println(n)
	return i
}
