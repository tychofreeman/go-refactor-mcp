package p

import "example.com/m/q"

func Plus(n int) int { return n + q.Base() }

func Use() int { return Plus(1) }
