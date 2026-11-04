package m

type Bed struct {
	Water  int
	Plants []string
}

func Totals(beds []Bed) (int, int) {
	water, plants := 0, 0
	for _, b := range beds {
		if b.Water < 0 {
			break
		}
		plants += len(b.Plants)
	}
	return water, plants
}
