package m

import "strings"

func label(s string) string { return "[" + strings.TrimSpace(s) + "]" }

func Use() string { return label(" a ") }
