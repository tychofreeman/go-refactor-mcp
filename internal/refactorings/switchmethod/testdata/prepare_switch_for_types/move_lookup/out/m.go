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
var kinds = map[string]Kind{"book": Book{}}

// Book is the Kind of "book".
type Book struct{}

func (Book) Shelf() string {
	return "stacks"
}

func (Book) Label(it Item) string {
	return strings.ToUpper(it.Title)
}

func Shelf(it Item) string {
	if k, ok := kinds[it.Kind]; ok {
		return k.Shelf()
	}
	switch it.Kind {
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
	default:
		return it.Title
	}
}
