package m

type Kind interface {
	Days() int
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
	switch k.(type) {
	case Book:
		shelves["stacks"]++
	case DVD:
		shelves["media"]++
	}
}
