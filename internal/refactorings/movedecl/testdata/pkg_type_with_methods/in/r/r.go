package r

import "example.com/m/p"

func Use() string {
	var c p.Counter
	c.Inc()
	return c.String()
}
