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
	if done, v, err := check(fail); done {
		return v, err
	}
	return 1, nil
}

func check(fail bool) (bool, int, error) {
	if fail {
		return true, 0, helper(fail)
	}
	return false, 0, nil
}
