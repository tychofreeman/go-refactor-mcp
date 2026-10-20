package m

type shape struct{ w, h int }

func fence(s shape) int { return s.perimeter() }
