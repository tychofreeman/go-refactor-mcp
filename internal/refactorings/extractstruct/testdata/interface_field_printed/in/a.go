package m

import "fmt"

type point struct{ x, y int }

type shape struct{ at point }

func show(s shape) { fmt.Printf("%+v\n", s) }
