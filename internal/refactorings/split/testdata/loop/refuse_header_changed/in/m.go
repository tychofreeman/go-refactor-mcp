package m

type Bed struct {
	Water  int
	Plants []string
}

func Totals(beds []Bed, n int) (int, int) {
	water, count := 0, 0
	for i := 0; i < n; i++ {
		water += beds[i].Water
		n--
	}
	return water, count
}
