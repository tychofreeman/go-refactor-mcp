package m

func classify(n int) string {
	if n < 0 {
		return "negative"
	}
	return describe(n)
}

func describe(n int) string {
	if n == 0 {
		return "zero"
	}
	if n < 10 {
		return "small"
	}
	return "large"
}
