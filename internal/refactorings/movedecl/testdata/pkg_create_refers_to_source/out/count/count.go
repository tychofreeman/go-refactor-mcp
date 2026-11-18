package count

import (
	"strings"

	"example.com/m/p"
)

// Two is one past Base, and its name.
func Two() (int, string) { return p.Base() + 1, strings.ToUpper("two") }
