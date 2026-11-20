package m

import "strings"

func withSizes(sizes []string, name string) string {
	switch len(sizes) {
	case 0:
		return name
	case 1:
		return name + " " + sizes[0]
	}
	return name + " (" + strings.Join(sizes, ", ") + ")"
}

func Label(plant string, beds, sizes []string) string {
	name := plant + " in " + strings.Join(beds, ", ")
	return withSizes(sizes, name)
}
