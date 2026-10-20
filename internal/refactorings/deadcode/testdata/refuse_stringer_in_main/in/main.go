package main

import "fmt"

type point struct{ x, y int }

func (p point) String() string { return fmt.Sprintf("(%d,%d)", p.x, p.y) }

func main() { fmt.Println(point{1, 2}) }
