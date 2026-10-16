package m

import "fmt"

func Describe(v any) string {
	switch val := v.(type) {
	case int:
		return fmt.Sprint(val + 1)
	case string:
		return val
	}
	return ""
}
