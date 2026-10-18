package m

import "strings"

func label(s, trimmed string) string { return "[" + trimmed + "]" }

func Use() string { return label(" a ", strings.TrimSpace(" a ")) }
