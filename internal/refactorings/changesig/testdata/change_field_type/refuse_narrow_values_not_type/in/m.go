package m

// Species is a kind of plant.
type Species interface{ Water() int }

// Moss is not a species.
type Moss struct{}

// Plant is something in a bed.
type Plant struct{ Species any }

// Rose is a fern.
var Rose = Plant{Species: "fern"}
