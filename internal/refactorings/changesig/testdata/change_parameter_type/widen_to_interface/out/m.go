package m

import (
	"bytes"
	"fmt"
	"io"
)

func Report(buf io.Writer, n int) {
	fmt.Fprintf(buf, "%d\n", n)
	buf.Write([]byte("done\n"))
}

func Run() string {
	var b bytes.Buffer
	Report(&b, 3)
	return b.String()
}
