package m

import "strings"

func greet(name string) string { return "hi " + name }

func Use(name string) string { return greet(name) + strings.TrimSpace(name) }
