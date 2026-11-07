package m

import "strconv"

func parse(s string) (int, bool, error) {
	if s == "" {
		return 0, false, nil
	}
	r1, r2 := strconv.Atoi(s)
	return r1, false, r2
}

func check(s string) error {
	return nil
}

func use() int {
	n, _, err := parse("1")
	if err != nil {
		return 0
	}
	var m, _, err2 = parse("2")
	_ = err2
	return n + m
}
