package m

import "errors"

func helper(fail bool) error {
	defer func() { recover() }()
	if fail {
		panic("x")
	}
	return errors.New("always")
}

func f(fail bool) (int, error) {
	println("start")
	if fail {
		return 0, helper(fail)
	}
	return 1, nil
}
