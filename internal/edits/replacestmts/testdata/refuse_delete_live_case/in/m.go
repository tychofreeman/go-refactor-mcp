package m

// Plant is something in a bed.
type Plant struct {
	Name    string
	Species any
}

// Water is how many litres p needs.
func Water(p Plant) int {
	switch p.Species {
	case "fern":
		return 3
	case "cactus":
		return 1
	}
	return 0
}

// Rose is the bed's first plant.
var Rose = Plant{"rose", "fern"}
