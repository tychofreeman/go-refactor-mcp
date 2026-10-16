package m

import (
	"io"
	"os"
)

func Copy(w io.Writer) error {
	_, err := io.Copy(w, os.Stdin)
	return err
}
