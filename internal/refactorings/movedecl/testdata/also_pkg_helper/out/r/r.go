package r

import (
	"example.com/m/p"
	"example.com/m/q"
)

func Use() int { return q.Two(" a ") + p.Keep() }
