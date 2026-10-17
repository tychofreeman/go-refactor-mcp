package m

import "fmt"

func run(s fmt.Stringer) {
	p := &s
	fmt.Println(s.String())
	fmt.Println(*p)
}
