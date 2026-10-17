package m

const limit = 10

func clamp(n int) int {
	if n > limit {
		return limit
	}
	return n
}

func Use() int { return clamp(20) }
