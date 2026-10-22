package m

import "fmt"

type C struct{ count int }

func bump(c *C) { c.count++ }

func show(c *C, n int) {
	fmt.Println(n)
	bump(c)
	fmt.Println(n)
}

func Use(c *C) {
	fmt.Println(c.count)
	bump(c)
	fmt.Println(c.count)
}
