package m

import "fmt"

// Sprinkler waters beds.
type Sprinkler struct{ rate int }

// waterArgs holds the arguments of water.
type waterArgs struct {
	bed    string
	litres int
}

// water gives bed litres of water at the sprinkler's rate.
func (s *Sprinkler) water(args waterArgs) {
	fmt.Println(args.bed, args.litres*s.rate)
}

func morning(s *Sprinkler) { s.water(waterArgs{bed: "roses", litres: 3}) }
