package m

// label has no side effects: its body only builds a string.
func label(s string) string { return "[" + s + "]" }

func greet(name string) string { return "hi " + name }

func Use(name string) string { return greet(name) }
