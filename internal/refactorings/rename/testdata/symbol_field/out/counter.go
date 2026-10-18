package m

type Counter struct {
	count int
	Name  string
}

func (c *Counter) inc() { c.count++ }

func NewCounter() *Counter {
	c := &Counter{count: 1, Name: "x"}
	c.inc()
	f := c.inc
	f()
	(*Counter).inc(c)
	return c
}
