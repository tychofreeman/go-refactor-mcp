package m

type kind string

const book kind = "book"

type Item struct{ Kind any }

func Days(it Item) int {
	switch it.Kind {
	case book:
		return 21
	case kind("dvd"):
		return 7
	}
	return 0
}

func Fragile(it Item) bool { return it.Kind == kind("atlas") }

var dvd = Item{Kind: kind("dvd")}
