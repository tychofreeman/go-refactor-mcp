package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Shelf() string
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
