package m

import "time"

func Seconds(a, b time.Duration) float64 {
	return (a + b).Seconds()
}
