package m

type Bed struct {
	Water  int
	Plants []string
}

func Totals(beds []Bed) (int, int) {
	water, count := 0, 0
	for i := 0; i < len(beds); i++ {
		water += beds[i].Water
	}
	for i := 0; i < len(beds); i++ {
		count++
	}
	return water, count
}
