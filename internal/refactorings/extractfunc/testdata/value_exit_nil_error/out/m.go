package m

import "fmt"

var ready bool

func run() (int, error) {
	if done, v, err := step(); done {
		return v, err
	}
	return 0, fmt.Errorf("not ready")
}

func step() (bool, int, error) {
	if ready {
		return true, 1, nil
	}
	fmt.Println("waiting")
	return false, 0, nil
}
