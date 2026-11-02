package m

import (
	"errors"
	"fmt"
)

func do() error { return errors.New("x") }

func run() error {
	err := do()
	if done, err := step(err); done {
		return err
	}
	return nil
}

func step(err error) (bool, error) {
	if err != nil {
		err = fmt.Errorf("wrapped: %w", err)
		return true, err
	}
	fmt.Println("x")
	return false, nil
}
