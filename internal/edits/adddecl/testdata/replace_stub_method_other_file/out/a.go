package m

type shape struct{ w, h int }

func (s shape) perimeter() int { return 2 * (s.w + s.h) }

func fence(s shape) int { return s.perimeter() }
