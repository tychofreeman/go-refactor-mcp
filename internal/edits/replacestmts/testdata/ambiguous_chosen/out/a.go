package m

import "math/rand"

func roll() int {
	return rand.Intn(6) + 1
}
