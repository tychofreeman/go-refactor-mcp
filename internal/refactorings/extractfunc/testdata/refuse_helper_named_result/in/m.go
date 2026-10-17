package m

import (
	"errors"
	"fmt"
)

// fail looks like it always fails, but its deferred function clears err.
func fail() (err error) {
	defer func() { err = nil }()
	return errors.New("failed")
}

func plan(name string) (string, error) {
	fmt.Println("planning")
	if name == "" {
		return "", fail()
	}
	fmt.Println("name ok")
	return name + "!", nil
}
