package m

type counter struct{ n int }

func (c *counter) Inc() {
	bump(c)
}

func bump(c *counter) {
	c.n++
	c.n *= 2
}
