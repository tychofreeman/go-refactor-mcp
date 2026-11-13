package m

func skip(next func() string) int {
	line := next()
	for line == "" {
		line = next()
	}
	return len(line)
}
