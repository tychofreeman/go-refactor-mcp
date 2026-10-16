package m

import "fmt"

func Describe(w, h int) string {
	if w*h > 100 {
		return fmt.Sprintf("large (%d)", w*h)
	}
	return fmt.Sprintf("small (%d)", w*h)
}
