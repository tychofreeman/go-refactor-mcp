package m

import "time"

func Wait() {
	const pause = 3 * time.Second
	time.Sleep(pause)
}
