package m

type Kind interface {
	Days() int
	shelve(shelves map[string]int)
}

type Book struct{}

func (Book) Days() int { return 21 }

type DVD struct{ Disc int }

func (DVD) Days() int { return 7 }

type Item struct {
	Title string
	Kind  Kind
}

func Shelve(k Kind, shelves map[string]int) {
	if k != nil {
		k.shelve(shelves)
	}
}

func (Book) shelve(shelves map[string]int) {
	shelves["stacks"]++
}

func (DVD) shelve(shelves map[string]int) {
	shelves["media"]++
}
