package m

import "fmt"

func double(n int) int { return n * 2 }

func F(x int) {
	fmt.Println(x)
	d := double(x)
	fmt.Println(d)
}
