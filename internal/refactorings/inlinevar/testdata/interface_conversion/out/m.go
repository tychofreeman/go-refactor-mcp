package m

import (
	"io"
	"os"
)

func NoInput() bool {
	return io.Reader(os.Stdin) == nil
}
