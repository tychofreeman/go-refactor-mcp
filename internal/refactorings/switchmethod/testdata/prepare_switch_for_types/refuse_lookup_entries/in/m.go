package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Shelf() string
}

type Book struct{}

func (Book) Shelf() string { return "stacks" }

var kinds = map[string]Kind{"book": Book{}}

func Fee(it Item, days int) int {
	switch it.Kind {
	case "book":
		return 10 * days
	}
	return 0
}
