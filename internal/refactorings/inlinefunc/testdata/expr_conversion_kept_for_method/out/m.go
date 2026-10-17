package m

import "fmt"

type seq func(yield func(int) bool)

func (s seq) first() int { return 0 }

func run() {
	fmt.Println(seq(func(yield func(int) bool) { yield(1) }).first())
}
