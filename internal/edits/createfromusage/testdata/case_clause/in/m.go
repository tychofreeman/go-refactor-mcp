package m

func f(s string, n int) int {
	switch {
	case isShort(s):
		return 1
	}
	switch n {
	case limit():
		return 2
	}
	return 0
}
