package m

type counter struct{ n int }

func (c *counter) add(by int) {
	c.n += by
}

func Use(c *counter) {
	c.add(2)
}
