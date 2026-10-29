package m

import "fmt"

type xy struct{ x, y int }

type point struct{ xy }

func show(p point) { fmt.Printf("%+v\n", p) }
