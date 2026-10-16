package m

import "errors"

func run(a, b int) ([]int, error) {
	if a < 0 {
		return nil, errors.New("a")
	}
	if b < 0 {
		return []int{}, errors.New("b")
	}
	a++
	return []int{a, b}, nil
}
