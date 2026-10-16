package m

import "errors"

func run(x int) (n int, err error) {
	if x < 0 {
		return 0, errors.New("negative")
	}
	n = x * 2
	return n, nil
}
