package m

// twice doubles n.
// It never fails.
func twice(n int) int { return 2 * n }

// Use calls helper first.
func Use() int { return twice(1) }
