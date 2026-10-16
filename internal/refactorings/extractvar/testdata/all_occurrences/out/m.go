package m

import "fmt"

func Describe(w, h int) string {
	area := w * h
	if area > 100 {
		return fmt.Sprintf("large (%d)", area)
	}
	return fmt.Sprintf("small (%d)", area)
}
