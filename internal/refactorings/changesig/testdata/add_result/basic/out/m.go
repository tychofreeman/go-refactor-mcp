package m

import "strconv"

func parse(s string) (int, error) {
	n, _ := strconv.Atoi(s)
	if n < 0 {
		return 0, nil
	}
	return n, nil
}

func use() int {
	v, _ := parse("1")
	parse("2")
	var w, _ = parse("3")
	defer parse("4")
	return v + w
}
