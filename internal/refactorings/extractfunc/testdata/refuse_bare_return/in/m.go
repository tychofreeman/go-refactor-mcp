package m

import "errors"

func run(x int) (n int, err error) {
	if x < 0 {
		err = errors.New("negative")
		return
	}
	n = x
	return
}
