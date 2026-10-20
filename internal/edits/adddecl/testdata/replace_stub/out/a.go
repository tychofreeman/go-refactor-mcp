package m

type shape struct{ w, h int }

// area is w times h.
func area(s shape) int { return s.w * s.h }

func size(s shape) int { return area(s) }
