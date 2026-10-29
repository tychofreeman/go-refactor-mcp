package m

// Plant is a plant.
type Plant struct{ Kind string }

func feed(p Plant) string { return "feed " + p.Kind }

// Feed feeds both.
func Feed(a, b Plant) string { return feed(a) + feed(b) }
