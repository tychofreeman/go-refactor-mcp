package m

import "time"

func wait(d time.Duration) time.Duration { return d }

func Use(n uint) time.Duration {
	time := 3
	_ = time
	return wait(1 << n)
}
