package m

import (
	"bytes"
	"fmt"
)

func Report(buf *bytes.Buffer, n int) {
	fmt.Fprintf(buf, "%d\n", n)
	buf.Write([]byte("done\n"))
}

func Run() string {
	var b bytes.Buffer
	Report(&b, 3)
	return b.String()
}
