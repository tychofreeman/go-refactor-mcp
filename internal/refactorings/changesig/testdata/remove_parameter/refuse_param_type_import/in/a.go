package m

import "io"

func f(n int, w io.Writer) int { return n }

var X = f(1, nil)
