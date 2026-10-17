package m

import "fmt"

type result struct {
	n   int
	msg string
}

func run(n int) result {
	if n <= 0 {
		return result{
			n:   n,
			msg: "done",
		}
	}
	fmt.Println(n)
	n *= 2
	return result{
		n:   n,
		msg: "done",
	}
}
