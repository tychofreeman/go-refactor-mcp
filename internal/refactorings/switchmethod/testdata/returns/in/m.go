package m

import "strings"

type Kind interface {
	Days() int
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
	switch k := it.Kind.(type) {
	case Book:
		return it.Title
	case DVD:
		if loud {
			return strings.ToUpper(it.Title)
		}
		// the disc number follows
		return "DVD " + it.Title + string(rune('0'+k.Disc))
	}
	return it.Title
}
