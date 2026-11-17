package b

import "example.com/m/a"

// Empty reports whether x has no road.
func Empty(x a.S) bool { return x.Routes["road"] == "" }

// First is the first shipment's road.
func First(xs []a.S) string { return xs[0].Routes["road"] }
