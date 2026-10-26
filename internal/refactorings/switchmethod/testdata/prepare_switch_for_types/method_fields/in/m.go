package m

type Species interface {
	Water(height int) int
}

type Plant struct {
	Height  int
	Species any
}

func Water(p Plant) int {
	switch p.Species {
	case "fern":
		return p.Height * 3
	case "cactus":
		return 1
	}
	return p.Height
}
