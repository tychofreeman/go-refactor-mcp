package m

import (
	"io"
	"os"
)

func NoInput() bool {
	var r io.Reader = os.Stdin
	return r == nil
}
