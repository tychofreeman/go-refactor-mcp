package m

type shape struct{ w, h int }

func size(s shape) int { return area(s) }

// area is w times h.
func area(s shape) int { return s.w * s.h }

func double(s shape) int { return 2 * size(s) }
