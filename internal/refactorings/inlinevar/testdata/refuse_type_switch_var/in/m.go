package m

func F(v any) int {
	switch n := v.(type) {
	case int:
		return n
	}
	return 0
}
