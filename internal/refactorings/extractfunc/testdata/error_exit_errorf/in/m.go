package m

import "fmt"

func half(n int) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("negative: %d", n)
	}
	h := n / 2
	fmt.Println(h)
	return h, nil
}
