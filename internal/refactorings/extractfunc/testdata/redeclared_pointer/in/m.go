package m

func count(xs []string) (int, string) { return len(xs), "" }

func plan(xs []string) (int, string) {
	why := ""
	note := func() string { return why }
	total := 0
	more, why := count(xs)
	total += more
	println(note())
	return total, why
}
