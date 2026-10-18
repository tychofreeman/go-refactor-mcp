package m

import "fmt"

func double(n int) int { return n * 2 }

func F(x int) {
	d := double(x)
	fmt.Println(x)
	fmt.Println(d)
}
