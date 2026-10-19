// Package strutil is a dependency of the module, outside it.
package strutil

import "strings"

// Up upper-cases s.
func Up(s string) string { return strings.ToUpper(s) }
