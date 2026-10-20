package m

func run() int { return count(3) }

func count(n int) int {
	if n == 0 {
		return 0
	}
	return 1 + count(n-1)
}
