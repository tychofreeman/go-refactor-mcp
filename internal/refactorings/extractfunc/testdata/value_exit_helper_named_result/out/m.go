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
	if done, v, err := checkName(name); done {
		return v, err
	}
	return name + "!", nil
}

func checkName(name string) (bool, string, error) {
	if name == "" {
		return true, "", fail()
	}
	fmt.Println("name ok")
	return false, "", nil
}
