package m

import "fmt"

type counter struct{ n int }

func run() {
	var c counter
	c.bump(2)
	fmt.Println(c.n)
}

func (c *counter) bump(by int) {
	c.n += by
	(*c).n++
	fmt.Println(c)
}
