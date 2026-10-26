package m

// Plant is something in a bed.
type Plant struct{ Name string }

func greet(p Plant, name string) string { return "hi " + name }

// Hello greets p.
func Hello(p Plant) string { return greet(p, p.Name) }
