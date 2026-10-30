package m

type C struct{ count int }

func (c *C) Get() int { return c.count }

func (c *C) Bump() int { c.count++; return 0 }

func double(n int) int { return n + n }

func around(n, m int) int { return n + m + n }

func f(c *C) int { return double(c.Get()) }
