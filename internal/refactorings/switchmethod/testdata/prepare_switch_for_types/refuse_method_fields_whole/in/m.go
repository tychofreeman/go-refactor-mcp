package m

type Species interface {
	Water(height int) int
}

type Plant struct {
	Height  int
	Species any
}

func tall(p Plant) bool { return p.Height > 10 }

func Water(p Plant) int {
	switch p.Species {
	case "fern":
		if tall(p) {
			return p.Height * 3
		}
		return 2
	}
	return 0
}
