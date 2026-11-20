package m

import "strings"

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Shelf() string
	Label(it Item) string
}

// kinds holds the Kind of each value whose cases have moved into a type.
var kinds = map[string]Kind{}

func Shelf(it Item) string {
	if k, ok := kinds[it.Kind]; ok {
		return k.Shelf()
	}
	switch it.Kind {
	case "book":
		return "stacks"
	case "dvd":
		return "media"
	}
	return "returns"
}

func Label(it Item) string {
	if k, ok := kinds[it.Kind]; ok {
		return k.Label(it)
	}
	switch it.Kind {
	case "dvd":
		return "DVD: " + it.Title
	case "book":
		return strings.ToUpper(it.Title)
	default:
		return it.Title
	}
}
