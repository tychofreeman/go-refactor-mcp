package m

func Name(n int) string {
	switch n {
	case 1:
		return "one"
	// two is the last one with a name
	case 2:
		return "two"
	}
	return "many"
}

func Size(x any) int {
	switch v := x.(type) {
	case int:
		return v
	case string:
		return len(v)
	}
	return 0
}
