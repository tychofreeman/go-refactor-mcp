package r

import "example.com/m/q"

func Use() string {
	var c q.Counter
	c.Inc()
	return c.String()
}
