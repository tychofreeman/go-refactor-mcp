package m

// Levels.
const (
	low = iota
	mid
	high
)

func Use() int { return low + mid + high }
