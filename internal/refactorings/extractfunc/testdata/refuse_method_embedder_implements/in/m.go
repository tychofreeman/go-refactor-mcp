package m

type stepper interface {
	step()
	reset()
}

type counter struct{ n int }

type wrapper struct {
	*counter
}

func (w wrapper) reset() {}

func (c *counter) Inc() {
	c.n++
}
