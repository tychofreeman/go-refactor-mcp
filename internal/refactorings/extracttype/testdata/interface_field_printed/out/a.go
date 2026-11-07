package m

import "fmt"

type xy struct{ x, y int }

type point struct{ xy }

type shape struct{ at point }

func show(s shape) { fmt.Printf("%+v\n", s) }
