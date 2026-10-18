package m

// helper doubles n.
// It never fails.
func helper(n int) int { return 2 * n }

// Use calls helper first.
func Use() int { return helper(1) }
