package m

// count is how many plants a bed of kind holds.
func count(kind string) int {
	if kind == "herb" {
		return 12
	}
	return 4
}

func plants(kinds []string) int {
	n := 0
	for _, k := range kinds {
		n += count(k)
	}
	return n
}
