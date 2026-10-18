package m

import "strings"

func shout(s string) string { return strings.ToUpper(s) + "!" }

func Use() string { return shout("a") }
