package m

import (
	"fmt"
	"slices"
)

func lookup(names []string, name string) (int, string) {
	fmt.Println("looking")
	if name == "" {
		return 0, "no name"
	}
	found := slices.Index(names, name)
	if found < 0 {
		return 0, fmt.Sprintf("no %s", name)
	}
	fmt.Println("found")
	return found, ""
}
