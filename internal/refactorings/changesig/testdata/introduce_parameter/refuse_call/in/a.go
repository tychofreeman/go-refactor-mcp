package m

import "fmt"

func f(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprint(s)
}

func Use() string { return f("a") }
