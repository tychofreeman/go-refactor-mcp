package m

type Counter struct {
	n   int64
	max int32
}

func (c *Counter) add(k int32) int32 {
	c.n += int64(k)
	c.n++
	return int32(c.n)
}
