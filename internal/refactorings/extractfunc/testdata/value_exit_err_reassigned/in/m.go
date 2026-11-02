package m

import (
	"errors"
	"fmt"
)

func do() error { return errors.New("x") }

func run() error {
	err := do()
	if err != nil {
		err = fmt.Errorf("wrapped: %w", err)
		return err
	}
	fmt.Println("x")
	return nil
}
