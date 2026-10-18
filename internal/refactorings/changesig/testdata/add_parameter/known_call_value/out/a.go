package m

import "strings"

func greet(name, upper string) string { return "hi " + name }

func Use(name string) string { return greet(name, strings.ToUpper(name)) + strings.TrimSpace(name) }
