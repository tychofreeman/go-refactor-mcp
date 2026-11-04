package m

import "fmt"

// dose holds the arguments of water.
type dose struct {
	bed    string
	litres int
}

// water gives bed litres of water.
func water(args dose) string {
	litres := args.litres
	if litres > 5 {
		litres = 5
	}
	return fmt.Sprint(args.bed, litres)
}

func morning() string {
	return water(dose{bed: "roses", litres: 3}) + water(dose{bed: "herbs", litres: 1})
}
