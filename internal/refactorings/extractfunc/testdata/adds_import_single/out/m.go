package m

import (
	"example.com/m/a"
	"example.com/m/b"
)

func run() int {
	v := a.Get()
	n := next(v)
	return n
}

func next(v b.T) int {
	n := v.N + 1
	return n
}
