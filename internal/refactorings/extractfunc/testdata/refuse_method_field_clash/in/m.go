package m

type counter struct{ n, step int }

func (c *counter) Inc() {
	c.n++
}
