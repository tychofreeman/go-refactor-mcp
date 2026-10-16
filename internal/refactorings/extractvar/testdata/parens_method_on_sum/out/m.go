package m

import "time"

func Seconds(a, b time.Duration) float64 {
	total := a + b
	return total.Seconds()
}
