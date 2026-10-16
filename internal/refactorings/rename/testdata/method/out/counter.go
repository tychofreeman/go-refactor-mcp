package m

type Counter struct {
	n    int
	Name string
}

func (c *Counter) increment() { c.n++ }

func NewCounter() *Counter {
	c := &Counter{n: 1, Name: "x"}
	c.increment()
	f := c.increment
	f()
	(*Counter).increment(c)
	return c
}
