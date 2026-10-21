package m

import "strconv"

func parse(s string) int {
	n, _ := strconv.Atoi(s)
	if n < 0 {
		return 0
	}
	return n
}

func use() int {
	v := parse("1")
	parse("2")
	var w = parse("3")
	defer parse("4")
	return v + w
}
