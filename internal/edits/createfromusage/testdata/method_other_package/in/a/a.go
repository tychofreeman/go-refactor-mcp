package a

import "example.com/m/b"

func Use(c *b.Counter) { c.Inc(1) }
