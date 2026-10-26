package m

type Cache struct{ v int }

func compute() int { return 42 }

func (c *Cache) Get() int {
	if c.v == 0 {
		c.v = compute()
	}
	return c.v
}

func f(c *Cache) int {
	v := c.Get()
	return v + v
}
