package m

import "strconv"

func parse(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func check(s string) error {
	return nil
}

func use() int {
	n, err := parse("1")
	if err != nil {
		return 0
	}
	var m, err2 = parse("2")
	_ = err2
	return n + m
}
