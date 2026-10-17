package m

func named(label string) string {
	return label
}

func Use(s []int) []string {
	return []string{named("a"), named("b"), named("c")}
}
