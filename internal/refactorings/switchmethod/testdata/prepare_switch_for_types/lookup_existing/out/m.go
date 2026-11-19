package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Shelf() string
	Fee(days int) int
}

var kinds = map[string]Kind{}

func Shelf(it Item) string {
	if k, ok := kinds[it.Kind]; ok {
		return k.Shelf()
	}
	return "returns"
}

func Fee(it Item, days int) int {
	if k, ok := kinds[it.Kind]; ok {
		return k.Fee(days)
	}
	switch it.Kind {
	case "book":
		return 10 * days
	case "dvd":
		return 50 * days
	case "atlas":
		return 50 * days
	}
	return 0
}
