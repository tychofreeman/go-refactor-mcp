package m

const (
	low = iota
	mid
	high
)

func Level() int { return low + high }
