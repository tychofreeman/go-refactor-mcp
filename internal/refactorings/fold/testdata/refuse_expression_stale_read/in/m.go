package m

type C struct{ count int }

func next(c *C) int {
	c.count++
	return 1
}

func plus(c *C, n int) int { return next(c) + n }

func Use(c *C) int { return next(c) + c.count }
