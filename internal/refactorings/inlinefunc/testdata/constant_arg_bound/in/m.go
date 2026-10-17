package m

import "fmt"

func run() {
	pad("x", 4)
}

func pad(s string, width int) {
	for len(s) < width {
		s += " "
	}
	fmt.Printf("[%s]\n", s)
}
