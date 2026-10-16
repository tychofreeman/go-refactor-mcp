package m

import "example.com/m/a"

func run() int {
	v := a.Get()
	n := v.N + 1
	return n
}
