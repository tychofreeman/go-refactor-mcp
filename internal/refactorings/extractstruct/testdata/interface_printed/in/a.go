package m

import "fmt"

type point struct{ x, y int }

func show(p point) { fmt.Printf("%+v\n", p) }
