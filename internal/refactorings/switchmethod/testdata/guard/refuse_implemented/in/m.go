package m

import "strings"

type Item struct {
	Title string
	Kind  any
}

func Label(it Item, loud bool) string {
	switch it.Kind {
	case "book":
		return it.Title
	case "dvd":
		if loud {
			return strings.ToUpper(it.Title)
		}
		return "DVD " + it.Title
	}
	return it.Title
}

type Book struct{}

func (Book) Label(it Item, loud bool) string { return "" }
