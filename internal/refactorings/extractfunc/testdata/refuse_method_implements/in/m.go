package m

type stepper interface {
	step()
	reset()
}

type counter struct{ n int }

func (c *counter) reset() { c.n = 0 }

func (c *counter) Inc() {
	c.n++
}
