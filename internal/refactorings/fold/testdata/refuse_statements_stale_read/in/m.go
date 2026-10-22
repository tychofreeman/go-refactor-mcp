package m

import "fmt"

type C struct{ count int }

func bump(c *C) { c.count++ }

func rec(c *C, n int) {
	bump(c)
	fmt.Println(n)
}

func Use(c *C) {
	bump(c)
	fmt.Println(c.count)
}
