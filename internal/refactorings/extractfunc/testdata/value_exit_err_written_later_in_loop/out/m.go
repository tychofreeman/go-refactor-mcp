package m

import "errors"

func g() error { return errors.New("x") }

func f() (int, error) {
	err := g()
	if err != nil {
		for i := 0; i < 2; i++ {
			if done, v, err := check(i, err); done {
				return v, err
			}
			err = nil
		}
	}
	return 1, nil
}

func check(i int, err error) (bool, int, error) {
	if i == 1 {
		return true, 0, err
	}
	return false, 0, nil
}
