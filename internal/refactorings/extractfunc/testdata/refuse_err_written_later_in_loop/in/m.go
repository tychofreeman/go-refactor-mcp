package m

import "errors"

func g() error { return errors.New("x") }

func f() (int, error) {
	err := g()
	if err != nil {
		for i := 0; i < 2; i++ {
			if i == 1 {
				return 0, err
			}
			err = nil
		}
	}
	return 1, nil
}
