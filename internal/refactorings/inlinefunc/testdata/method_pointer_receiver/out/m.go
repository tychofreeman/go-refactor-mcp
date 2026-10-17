package m

import "fmt"

type counter struct{ n int }

func (c *counter) run() {
	c.n += 2
	fmt.Println("bumped")
	fmt.Println(c.n)
}
