package m

func Kind(n, lim int) string {
	switch {
	case n > lim*2:
		return "big"
	}
	return "small"
}
