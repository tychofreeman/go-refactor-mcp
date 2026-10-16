package m

func pick(n int) string {
	switch n = n * 2; n % 3 {
	case 0:
		return "zero"
	}
	return "other"
}
