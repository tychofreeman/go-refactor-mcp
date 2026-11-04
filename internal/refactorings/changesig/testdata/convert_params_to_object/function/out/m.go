package m

import "fmt"

// waterArgs holds the arguments of water.
type waterArgs struct {
	bed    string
	litres int
}

// water gives bed litres of water.
func water(args waterArgs) string {
	litres := args.litres
	if litres > 5 {
		litres = 5
	}
	return fmt.Sprint(args.bed, litres)
}

func morning() string {
	return water(waterArgs{bed: "roses", litres: 3}) + water(waterArgs{bed: "herbs", litres: 1})
}
