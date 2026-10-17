package m

import (
	"bytes"
	"fmt"
)

func run() {
	var b bytes.Buffer
	step(&b)
	fmt.Println(b.String())
}

func step(b *bytes.Buffer) {
	b.WriteString("a")
}
