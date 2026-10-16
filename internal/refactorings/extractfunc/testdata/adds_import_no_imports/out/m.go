package m

import "example.com/m/b"

func run() int {
	v := current
	n := double(v)
	return n
}

func double(v b.T) int {
	n := v.N * 2
	return n
}
