package m

func plants(kinds []string) int {
	n := 0
	for _, k := range kinds {
		var result int
		if k == "herb" {
			result = 12
		} else {
			result = 4
		}
		n += result
	}
	return n
}
