package m

type Item struct {
	Title string
	Kind  string
}

func Shelf(it Item) string {
	switch it.Kind {
	case "book":
		return "stacks"
	case "dvd":
		return "media"
	}
	return "returns"
}
