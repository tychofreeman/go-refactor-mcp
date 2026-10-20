package main

import "fmt"

type point struct{ x, y int }

func (p point) Describe(prefix string) string { return prefix }

func main() { fmt.Println(point{1, 2}) }
