package m

func count(xs []string) (int, string) { return len(xs), "" }

func plan(xs []string) (int, string) {
	why := ""
	note := func() string { return why }
	total := step(&why, xs)
	println(note())
	return total, why
}

func step(why *string, xs []string) int {
	total := 0
	var more int
	more, *why = count(xs)
	total += more
	return total
}
