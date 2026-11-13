package m

import "strings"

func grow(names []string) []string {
	for i := 0; i < len(names); i++ {
		if strings.HasPrefix(names[i], "x") {
			names = append(names, "y")
		}
	}
	return names
}
