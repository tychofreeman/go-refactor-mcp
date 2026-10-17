package m

type counter struct{ n int }

func (c *counter) step() {}

func (c *counter) Inc() {
	c.n++
}
