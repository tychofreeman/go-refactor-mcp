package q

import "example.com/m/p"

func Quad(n int) int { return p.Double(p.Double(n)) }
