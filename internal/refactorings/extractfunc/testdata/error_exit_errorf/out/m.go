package m

import "fmt"

func half(n int) (int, error) {
	h, err := halve(n)
	if err != nil {
		return 0, err
	}
	fmt.Println(h)
	return h, nil
}

func halve(n int) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("negative: %d", n)
	}
	h := n / 2
	return h, nil
}
