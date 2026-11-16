package a

// Config says how to run.
type Config struct{ n int }

// Dispatch runs x.
func (c Config) Dispatch(x int) int { return c.n + x }
