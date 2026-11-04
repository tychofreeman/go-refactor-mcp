package m

type Bed struct {
	Water  int
	Plants []string
}

func Totals(beds []Bed) (int, int) {
	water, plants := 0, 0
	for _, b := range beds {
		water += b.Water
	}
	for _, b := range beds {
		// Count the plants too.
		plants += len(b.Plants)
	}
	return water, plants
}
