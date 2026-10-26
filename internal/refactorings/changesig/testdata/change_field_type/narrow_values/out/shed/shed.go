package shed

import "example.com/g/garden"

// Pot is the potted plant.
var Pot = garden.Plant{Species: garden.Cactus{}}

// IsFern reports whether p is a fern.
func IsFern(p garden.Plant) bool { return p.Species == (garden.Fern{}) }
