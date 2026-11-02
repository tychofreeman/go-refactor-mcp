package m

import "fmt"

var ready bool

func run() (int, error) {
	if ready {
		return 1, nil
	}
	fmt.Println("waiting")
	return 0, fmt.Errorf("not ready")
}
