package m

import "fmt"

type counter struct{ n int }

func (c *counter) run() {
	c.bump(2)
	fmt.Println(c.n)
}

func (c *counter) bump(by int) {
	c.n += by
	fmt.Println("bumped")
}
