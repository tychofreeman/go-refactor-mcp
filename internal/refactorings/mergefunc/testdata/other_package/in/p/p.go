package p

import "fmt"

func Count(n int) string { return fmt.Sprintf("%d item%s", n, plural(n)) }

func suffix() func(int) string { return plural }

func plural(n int) string {
	// one is singular
	if n == 1 {
		return ""
	}
	return "s"
}
