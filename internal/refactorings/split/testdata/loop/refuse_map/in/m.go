package m

func Names(beds map[string]int) ([]string, []int) {
	var names []string
	var waters []int
	for name, w := range beds {
		names = append(names, name)
		waters = append(waters, w)
	}
	return names, waters
}
