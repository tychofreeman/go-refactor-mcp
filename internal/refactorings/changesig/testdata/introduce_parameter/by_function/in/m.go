package m

// Plant is something in a bed.
type Plant struct{ Name string }

func greet(p Plant) string { return "hi " + p.Name }

// Hello greets p.
func Hello(p Plant) string { return greet(p) }
