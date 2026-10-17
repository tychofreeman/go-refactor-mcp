package m

import "fmt"

var counter int

func next() int { counter++; return counter }

func run() {
	v := next()
	fmt.Println(v)
	fmt.Println(v)
}
