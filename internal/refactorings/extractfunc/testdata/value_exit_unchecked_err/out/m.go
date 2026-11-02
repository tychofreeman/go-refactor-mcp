package m

import (
	"errors"
	"fmt"
)

func do() error { return errors.New("x") }

func run(flag bool) error {
	err := do()
	if done, err := step(flag, err); done {
		return err
	}
	return nil
}

func step(flag bool, err error) (bool, error) {
	if flag {
		return true, err
	}
	fmt.Println("x")
	return false, nil
}
