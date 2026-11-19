package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Shelf() string
}

var kinds = map[string]Kind{}

func Shelf(it Item) string {
	if k, ok := kinds[it.Kind]; ok {
		return k.Shelf()
	}
	return "returns"
}

func Fee(it Item, days int) int {
	switch it.Kind {
	case "book":
		return 10 * days
	case "dvd", "atlas":
		return 50 * days
	}
	return 0
}
