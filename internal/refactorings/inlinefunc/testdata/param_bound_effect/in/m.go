package m

import "fmt"

var counter int

func next() int { counter++; return counter }

func run() {
	twice(next())
}

func twice(v int) {
	fmt.Println(v)
	fmt.Println(v)
}
