package m

func F(k int) string {
	switch k {
	case 0:
		fallthrough
	case 1:
		return "one"
	}
	return "other"
}
