package m

import "strings"

type Kind interface {
	Days() int
	label(it Item, loud bool) string
}

type Book struct{}

func (Book) Days() int { return 21 }

type DVD struct{ Disc int }

func (DVD) Days() int { return 7 }

type Item struct {
	Title string
	Kind  Kind
}

// Label is how an item is shown.
func Label(it Item, loud bool) string {
	if it.Kind != nil {
		return it.Kind.label(it, loud)
	}
	return it.Title
}

func (Book) label(it Item, loud bool) string {
	return it.Title
}

func (k DVD) label(it Item, loud bool) string {
	if loud {
		return strings.ToUpper(it.Title)
	}
	// the disc number follows
	return "DVD " + it.Title + string(rune('0'+k.Disc))
}
