package a

// Config says how to run.
type Config struct{ n int }

// Dispatch runs x.
func Dispatch(c Config, x int) int { return c.n + x }
