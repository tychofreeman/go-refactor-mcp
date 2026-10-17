package m

import "errors"

func run(n int) (err error) {
	if n > 0 {
		check(n)
		return
	}
	return nil
}

func check(n int) {
	if n > 10 {
		err := errors.New("big")
		_ = err
		return
	}
	println(n)
}
