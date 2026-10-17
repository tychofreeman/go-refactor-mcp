package m

import "fmt"

func run(s fmt.Stringer) {
	p := &s
	show(&s)
	fmt.Println(*p)
}

func show(s *fmt.Stringer) {
	fmt.Println((*s).String())
}
