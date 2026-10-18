package m

import "strings"

// Settings.
var (
	a = 1
	// b collects text.
	b *strings.Builder // trailing
	c = 3
)

func Use() int { return a + len(b.String()) + c }
