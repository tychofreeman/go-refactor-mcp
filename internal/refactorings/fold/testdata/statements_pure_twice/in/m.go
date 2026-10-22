package m

import (
	"fmt"
	"log"
)

func last(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func show(name string, n int) {
	log.Printf("%s: %d", name, n)
	fmt.Println(name)
}

func Use(s string) {
	log.Printf("%s: %d", last(s, 4), 1)
	fmt.Println(last(s, 4))
}

func Later(s string, xs []int) {
	log.Printf("%s: %d", s, xs[0])
	fmt.Println(s)
}
