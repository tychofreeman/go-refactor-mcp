package m

type Item struct {
	Title string
	Kind  string
}

type Kind interface {
	Renewable() bool
	Shelve(shelves map[string]int)
}

var kinds = map[string]Kind{"book": Book{}, "atlas": Atlas{}}

// Atlas is the Kind of "atlas".
type Atlas struct{}

func (Atlas) Renewable() bool {
	return false
}

func (Atlas) Shelve(shelves map[string]int) {
}

type Book struct{}

func (Book) Renewable() bool { return true }

func (Book) Shelve(shelves map[string]int) { shelves["stacks"]++ }

func Renewable(it Item) bool {
	if k, ok := kinds[it.Kind]; ok {
		return k.Renewable()
	}
	switch it.Kind {
	case "dvd":
		return true
	}
	return false
}

func Shelve(it Item, shelves map[string]int) {
	if k, ok := kinds[it.Kind]; ok {
		k.Shelve(shelves)
	} else {
		switch it.Kind {
		case "dvd":
			shelves["media"]++
		}
	}
}
