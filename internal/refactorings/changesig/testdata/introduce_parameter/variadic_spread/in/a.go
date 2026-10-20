package m

import "strings"

func f(sep string, parts ...string) string {
	return strings.Join(parts, sep)
}

func Use(xs []string) string { return f(",", xs...) }
