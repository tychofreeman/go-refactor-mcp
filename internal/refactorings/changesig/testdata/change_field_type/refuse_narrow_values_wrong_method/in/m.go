package m

// Species is a kind of plant.
type Species interface{ Water(p Plant) int }

// Fern is a species.
type Fern struct{}

func (Fern) Water(days int) int { return 3 * days }

// Plant is something in a bed.
type Plant struct{ Species any }

// Rose is a fern.
var Rose = Plant{Species: "fern"}
