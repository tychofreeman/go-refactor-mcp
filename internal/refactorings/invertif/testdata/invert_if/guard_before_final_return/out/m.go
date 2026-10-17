package m

import (
	"errors"
	"fmt"
)

func step() error { return errors.New("x") }

func run() error {
	err := step()
	if err != nil {
		return err
	}
	fmt.Println("a")
	fmt.Println("b")
	return err
}
