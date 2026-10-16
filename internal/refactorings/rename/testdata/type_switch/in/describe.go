package m

import "fmt"

func Describe(v any) string {
	switch x := v.(type) {
	case int:
		return fmt.Sprint(x + 1)
	case string:
		return x
	}
	return ""
}
