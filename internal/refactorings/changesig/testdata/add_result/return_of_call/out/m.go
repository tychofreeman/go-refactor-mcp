package m

import "strconv"

func lookup(k string) (int, bool, error) {
	if k != "" {
		r1, r2 := strconv.Atoi(k)
		return r1, true, r2
	}
	return 1, true, nil
}

func use() {
	n, _, err := lookup("2")
	println(n, err)
}
