package m

import (
	"errors"
	"fmt"
)

func step() error { return errors.New("x") }

func run() {
	err := step()
	fmt.Println(err)
	if err := step(); err == nil {
		fmt.Println(err)
	}
}
