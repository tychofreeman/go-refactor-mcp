package m

import "example.com/m/q"

var (
	Host = "localhost"
)

func Addr() (string, int) { return Host, q.Port }
