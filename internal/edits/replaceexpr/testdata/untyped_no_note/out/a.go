package m

func isSet(n int) bool { return n > 0 }

func f(n int) int {
	if isSet(n) {
		return 1
	}
	return 0
}
