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
	switch it.Kind {
	case "book":
		return "stacks"
	}
	return "returns"
}
