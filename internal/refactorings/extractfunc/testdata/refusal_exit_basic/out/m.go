package m

import (
	"fmt"
	"slices"
)

func lookup(names []string, name string) (int, string) {
	fmt.Println("looking")
	found, why := find(name, names)
	if why != "" {
		return 0, why
	}
	fmt.Println("found")
	return found, ""
}

func find(name string, names []string) (int, string) {
	if name == "" {
		return 0, "no name"
	}
	found := slices.Index(names, name)
	if found < 0 {
		return 0, fmt.Sprintf("no %s", name)
	}
	return found, ""
}
