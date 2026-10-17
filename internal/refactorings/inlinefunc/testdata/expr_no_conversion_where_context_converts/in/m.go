package m

import "fmt"

type seq func(yield func(int) bool)

func (s seq) first() int { return 0 }

func count() seq {
	return func(yield func(int) bool) { yield(1) }
}

func run() {
	for v := range count() {
		fmt.Println(v)
	}
}
