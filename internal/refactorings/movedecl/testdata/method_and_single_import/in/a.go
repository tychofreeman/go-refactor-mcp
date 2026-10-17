package m

import "strings"

type T struct{ s string }

func (t T) Upper() string { return strings.ToUpper(t.s) }

func (t T) Len() int { return len(t.s) }
