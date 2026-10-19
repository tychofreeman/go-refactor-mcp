package m

import "strconv"

type T struct{ n int }

func (t T) Label() string {
	if t.n == 0 {
		return "zero"
	}
	return strconv.Itoa(t.n)
}

func Double(n int) int { return n }
