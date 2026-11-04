package m

type Bed struct {
	Water  int
	Plants []string
}

func Totals(beds []Bed) (int, int) {
	water, most := 0, 0
	for _, b := range beds {
		water += b.Water
		most = max(most, water)
	}
	return water, most
}
