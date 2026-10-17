package m

import "fmt"

type greeter interface{ greet() }

type t struct{}

func (t) greet() { fmt.Println("hi") }

func run(v t) {
	fmt.Println("hi")
}
