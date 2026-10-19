package m

import "io"

type Counter struct{ n int }

var _ io.Writer = (*Counter)(nil)
