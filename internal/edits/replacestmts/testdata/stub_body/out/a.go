package m

import "strings"

// Shout upper-cases s and adds a bang.
func Shout(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return strings.ToUpper(s) + "!", nil
}
