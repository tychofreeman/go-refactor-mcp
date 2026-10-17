package m

type counter struct{ n int }

func (c *counter) add(note string, by int) {
	c.n += by
}

func Use(c *counter) {
	c.add("two", 2)
}
