package b

import "example.com/m/a"

// Make returns a Config.
func Make() a.Config { return a.Config{} }

// Run runs 1.
func Run(c a.Config) int { return a.Dispatch(c, 1) }
