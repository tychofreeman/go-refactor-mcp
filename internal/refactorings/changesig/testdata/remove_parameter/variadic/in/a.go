package m

func named(label string, xs ...int) string {
	return label
}

func Use(s []int) []string {
	return []string{named("a"), named("b", 1, 2), named("c", s...)}
}
