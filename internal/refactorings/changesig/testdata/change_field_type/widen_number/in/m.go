package m

type Counter struct{ n, max int32 }

func (c *Counter) add(k int32) int32 {
	c.n += k
	c.n++
	return c.n
}
