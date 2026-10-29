package m

import "fmt"

type point struct{ x, y int }

func show(ps []point) { fmt.Printf("%+v\n", ps) }
