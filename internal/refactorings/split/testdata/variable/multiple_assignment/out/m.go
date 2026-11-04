package m

import "strconv"

func Sizes(a, b string) (int, error) {
	size, err := strconv.Atoi(a)
	if err != nil {
		return size, err
	}
	var potSize int
	potSize, err = strconv.Atoi(b)
	return potSize, err
}
