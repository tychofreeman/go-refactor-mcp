package m

import "strings"

func upper(names []string) {
	for i := 0; i < len(names); i++ {
		names[i] = strings.ToUpper(names[i])
	}
}
