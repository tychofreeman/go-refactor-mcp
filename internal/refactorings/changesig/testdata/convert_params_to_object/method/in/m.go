package m

import "fmt"

// Sprinkler waters beds.
type Sprinkler struct{ rate int }

// water gives bed litres of water at the sprinkler's rate.
func (s *Sprinkler) water(bed string, litres int) {
	fmt.Println(bed, litres*s.rate)
}

func morning(s *Sprinkler) { s.water("roses", 3) }
