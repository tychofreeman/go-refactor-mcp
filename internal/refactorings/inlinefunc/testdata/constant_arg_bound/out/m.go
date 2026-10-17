package m

import "fmt"

func run() {
	s, width := "x", 4
	for len(s) < width {
		s += " "
	}
	fmt.Printf("[%s]\n", s)
}
