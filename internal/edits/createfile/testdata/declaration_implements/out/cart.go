package m

import (
	"fmt"
	"io"
)

type Cart struct{}

var (
	_ Priced    = (*Cart)(nil)
	_ io.Writer = (*Cart)(nil)
)

func (c *Cart) Label(prefix string) (fmt.Stringer, error) {
	panic("not implemented")
}

func (c *Cart) Price() int {
	panic("not implemented")
}

func (c *Cart) Write(p []byte) (n int, err error) {
	panic("not implemented")
}
