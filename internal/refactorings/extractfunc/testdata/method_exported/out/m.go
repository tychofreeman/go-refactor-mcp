package m

type counter struct{ n int }

func (c *counter) Inc() {
	c.Bump()
}

func (c *counter) Bump() {
	c.n++
}
