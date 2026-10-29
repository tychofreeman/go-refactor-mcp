package m

// Plant is a plant.
type Plant struct{ Kind string }

func feed(p Plant, kind string) string {
	kind = "a " + kind
	return "feed " + kind
}

// Feed feeds both.
func Feed(a, b Plant) string { return feed(a, a.Kind) + feed(b, b.Kind) }
