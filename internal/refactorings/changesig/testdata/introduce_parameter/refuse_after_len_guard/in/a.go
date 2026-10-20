package m

func pick(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	return xs[0]
}

func Use(xs []int) int { return pick(xs) }
