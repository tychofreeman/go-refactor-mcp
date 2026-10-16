package m

import (
	"bytes"
	"fmt"
)

func run() {
	var b bytes.Buffer
	b.WriteString("a")
	fmt.Println(b.String())
}
