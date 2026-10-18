package m

type Counter struct {
	n    int
	Name string
}

func (c *Counter) inc() { c.n++ }

func NewCounter() *Counter {
	c := &Counter{n: 1, Name: "x"}
	c.inc()
	f := c.inc
	f()
	(*Counter).inc(c)
	return c
}
