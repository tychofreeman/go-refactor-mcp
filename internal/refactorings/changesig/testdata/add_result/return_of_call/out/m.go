package m

import "strconv"

func lookup(k string) (int, error, bool) {
	if k != "" {
		r1, r2 := strconv.Atoi(k)
		return r1, r2, true
	}
	return 1, nil, true
}

func use() {
	n, err, _ := lookup("2")
	println(n, err)
}
