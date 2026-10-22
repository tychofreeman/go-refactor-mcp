package m

type kind string

const book kind = "book"

type Item struct{ Kind kind }

func Days(it Item) int {
	switch it.Kind {
	case book:
		return 21
	case "dvd":
		return 7
	}
	return 0
}

func Fragile(it Item) bool { return it.Kind == "atlas" }

var dvd = Item{Kind: "dvd"}
