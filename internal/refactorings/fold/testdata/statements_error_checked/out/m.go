package m

import "strconv"

func parse(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func Double(s string) (int, error) {
	v, err := parse(s)
	if err != nil {
		return 0, err
	}
	return v * 2, nil
}
