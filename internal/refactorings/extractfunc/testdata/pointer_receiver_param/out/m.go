package m

type counter struct{ n int }

func (c *counter) Inc() {
	c.bump()
}

func (c *counter) bump() {
	c.n++
	c.n *= 2
}
