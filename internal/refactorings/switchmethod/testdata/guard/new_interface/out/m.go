package m

import "strings"

type Item struct {
	Title string
	Kind  any
}

type Kind interface {
	Label(it Item, loud bool) string
}

func Label(it Item, loud bool) string {
	if k, ok := it.Kind.(Kind); ok {
		return k.Label(it, loud)
	}
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
