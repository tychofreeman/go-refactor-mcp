package m

import "strconv"

func Sizes(a, b string) (int, error) {
	size, err := strconv.Atoi(a)
	if err != nil {
		return size, err
	}
	size, err = strconv.Atoi(b)
	return size, err
}
