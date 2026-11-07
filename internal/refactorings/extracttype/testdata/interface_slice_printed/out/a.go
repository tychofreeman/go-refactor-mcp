package m

import "fmt"

type xy struct{ x, y int }

type point struct{ xy }

func show(ps []point) { fmt.Printf("%+v\n", ps) }
