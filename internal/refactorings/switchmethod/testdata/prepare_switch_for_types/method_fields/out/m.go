package m

type Species interface {
	Water(height int) int
}

type Plant struct {
	Height  int
	Species any
}

func Water(p Plant) int {
	height := p.Height
	if s, ok := p.Species.(Species); ok {
		return s.Water(height)
	}
	switch p.Species {
	case "fern":
		return height * 3
	case "cactus":
		return 1
	}
	return p.Height
}
