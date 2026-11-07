package m

type shape struct{ w, h int }

func size(s shape) int { return area(s) }

func area(s shape) int {
	panic("not implemented")
}

func double(s shape) int { return 2 * size(s) }
