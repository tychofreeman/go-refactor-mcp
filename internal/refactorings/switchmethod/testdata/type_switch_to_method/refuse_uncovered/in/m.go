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

type Atlas struct{}

func (Atlas) Days() int { return 3 }

func F(k Kind) int {
	switch k.(type) {
	case Book:
		return 1
	case DVD:
		return 2
	}
	return 0
}
