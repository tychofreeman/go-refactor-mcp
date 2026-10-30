package m

import "strconv"

func lookup(k string) (int, error) {
	if k != "" {
		return strconv.Atoi(k)
	}
	return 1, nil
}

func use() {
	n, err := lookup("2")
	println(n, err)
}
