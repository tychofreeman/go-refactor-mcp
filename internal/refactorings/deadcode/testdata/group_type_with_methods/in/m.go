package m

import "strings"

// F is used.
func F() int { return 1 }

type shout struct{ s string }

func (x shout) loud() string { return strings.ToUpper(x.s) }

func (x shout) twice() string { return x.loud() + x.loud() }
