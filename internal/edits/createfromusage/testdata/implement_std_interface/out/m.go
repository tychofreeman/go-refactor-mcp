package m

import "io"

type Counter struct{ n int }

var _ io.Writer = (*Counter)(nil)

func (c *Counter) Write(p []byte) (n int, err error) {
	panic("not implemented")
}
