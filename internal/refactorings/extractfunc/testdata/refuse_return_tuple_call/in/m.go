package m

import "strconv"

func run(s string) (int, error) {
	if s == "" {
		return strconv.Atoi("0")
	}
	s += "1"
	return strconv.Atoi(s)
}
