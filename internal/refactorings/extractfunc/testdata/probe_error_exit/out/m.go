package main

import "fmt"

func total(xs []int) (int, error) {
	sum, err := add(xs)
	if err != nil {
		return 0, err
	}
	return sum, nil
}

func add(xs []int) (int, error) {
	sum := 0
	for _, x := range xs {
		if x < 0 {
			return 0, fmt.Errorf("negative %d", x)
		}
		sum += x
	}
	return sum, nil
}

func main() {
	fmt.Println(total([]int{1, 2}))
	fmt.Println(total([]int{1, -1, 5}))
}
