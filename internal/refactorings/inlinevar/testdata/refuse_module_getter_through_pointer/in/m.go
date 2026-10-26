package m

type Counter struct{ n int }

func (c *Counter) Value() int { return c.n }

func f(c *Counter) int {
	v := c.Value()
	return v + v
}
