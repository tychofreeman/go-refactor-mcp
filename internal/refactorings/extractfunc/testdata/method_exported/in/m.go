package m

type counter struct{ n int }

func (c *counter) Inc() {
	c.n++
}
