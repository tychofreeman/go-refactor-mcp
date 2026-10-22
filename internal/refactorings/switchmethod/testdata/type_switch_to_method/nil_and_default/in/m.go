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

func Fee(k Kind) int {
	switch k.(type) {
	case nil:
		return -1
	case Book:
		return 10
	case DVD:
		return 30
	default:
		return 0
	}
}
