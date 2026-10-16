package m

import (
	"io"
	"os"
)

func Copy(w io.Writer) error {
	var r io.Reader = os.Stdin
	_, err := io.Copy(w, r)
	return err
}
