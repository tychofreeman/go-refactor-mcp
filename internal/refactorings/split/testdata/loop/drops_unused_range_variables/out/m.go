package m

type Bed struct {
	Water  int
	Plants []string
}

func Totals(beds []Bed) (int, int) {
	water, even := 0, 0
	for _, b := range beds {
		water += b.Water
	}
	for i := range beds {
		if i%2 == 0 {
			even++
		}
	}
	return water, even
}
