package m

import "fmt"

type C struct{ count int }

func bump(c *C) { c.count++ }

func rec(c *C, n int) {
	fmt.Println(n)
	bump(c)
}

func Use(c *C) {
	rec(c, c.count)
}
