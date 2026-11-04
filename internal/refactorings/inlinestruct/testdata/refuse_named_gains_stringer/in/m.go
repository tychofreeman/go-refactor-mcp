package m

import "fmt"

// Soil is what a bed is filled with.
type Soil struct{ depth int }

// String names the soil.
func (s Soil) String() string { return fmt.Sprint(s.depth, "cm") }

// Bed is a bed of plants.
type Bed struct {
	soil Soil
	name string
}

func show(b Bed) { fmt.Println(b.soil, b.name) }
