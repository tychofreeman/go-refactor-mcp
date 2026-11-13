package m

func funcs(names []string) []func() int {
	var fs []func() int
	for i := 0; i < len(names); i++ {
		fs = append(fs, func() int { return len(names[i]) + i })
	}
	return fs
}
