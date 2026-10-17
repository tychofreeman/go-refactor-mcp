package m

import "strings"

// loud upper-cases s.
func loud(s string) string {
	return strings.ToUpper(s) + "!"
}
