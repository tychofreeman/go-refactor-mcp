package m

import "fmt"

type point struct{ x, y int }

func (p point) text() string { return fmt.Sprintf("(%d,%d)", p.x, p.y) }

func Show(p point) string { return fmt.Sprint(p) + p.text() }
