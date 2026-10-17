package m

type base struct{}

func (base) step() {}

type counter struct {
	base
	n int
}

func (c *counter) Inc() {
	c.n++
}
