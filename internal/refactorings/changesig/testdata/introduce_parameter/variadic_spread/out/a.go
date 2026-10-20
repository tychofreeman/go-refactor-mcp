package m

import "strings"

func f(sep string, s string, parts ...string) string {
	return s
}

func Use(xs []string) string { return f(",", strings.Join(xs, ","), xs...) }
