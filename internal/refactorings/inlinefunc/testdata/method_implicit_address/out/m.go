package m

import "fmt"

type counter struct{ n int }

func run() {
	var c counter
	c.n += 2
	c.n++
	fmt.Println(&c)
	fmt.Println(c.n)
}
