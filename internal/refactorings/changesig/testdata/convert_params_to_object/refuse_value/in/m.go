package m

import "fmt"

// water gives bed litres of water.
func water(bed string, litres int) string {
	if litres > 5 {
		litres = 5
	}
	return fmt.Sprint(bed, litres)
}

func morning() string { return water("roses", 3) + water("herbs", 1) }

var waterer = water
