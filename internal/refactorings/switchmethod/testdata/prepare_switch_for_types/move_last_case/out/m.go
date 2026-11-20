package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Renewable() bool
	Shelve(shelves map[string]int)
}

var kinds = map[string]Kind{"book": Book{}, "dvd": DVD{}}

// DVD is the Kind of "dvd".
type DVD struct{}

func (DVD) Renewable() bool {
	return true
}

func (DVD) Shelve(shelves map[string]int) {
	shelves["media"]++
}

type Book struct{}

func (Book) Renewable() bool { return true }

func (Book) Shelve(shelves map[string]int) { shelves["stacks"]++ }

func Renewable(it Item) bool {
	if k, ok := kinds[it.Kind]; ok {
		return k.Renewable()
	}
	return false
}

func Shelve(it Item, shelves map[string]int) {
	if k, ok := kinds[it.Kind]; ok {
		k.Shelve(shelves)
	}
}
