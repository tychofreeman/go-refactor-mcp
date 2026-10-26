package m

// Species is a kind of plant.
type Species interface{ Water() int }

// Fern is a species.
type Fern struct{}

func (Fern) Water() int { return 3 }

// Plant is something in a bed.
type Plant struct{ Species any }

// Parse makes a plant of a species' name.
func Parse(name string) Plant { return Plant{Species: name} }

// Rose is a fern.
var Rose = Plant{Species: "fern"}

// Moss is not.
var Moss = Plant{Species: "moss"}
