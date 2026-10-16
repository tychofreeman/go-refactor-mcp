package m

import "strconv"

func parse(s string) (int, error) {
	s = s + "0"
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n * 2, nil
}
