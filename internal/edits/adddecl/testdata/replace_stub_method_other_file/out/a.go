package m

type shape struct{ w, h int }

func fence(s shape) int { return s.perimeter() }

func (s shape) perimeter() int { return 2 * (s.w + s.h) }
