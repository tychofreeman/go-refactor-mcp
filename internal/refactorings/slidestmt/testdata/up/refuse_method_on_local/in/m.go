package m

type counter struct{ n int }

func (c *counter) inc() { c.n++ }

func F() int {
	var c counter
	k := c.n
	c.inc()
	return k
}
