package m

var calls int

// count has a side effect: it changes a package variable.
func count(s string) string { calls++; return s }

func f(s string) string {
	if s == "" {
		return ""
	}
	return count(s)
}

func Use() string { return f("a") }
