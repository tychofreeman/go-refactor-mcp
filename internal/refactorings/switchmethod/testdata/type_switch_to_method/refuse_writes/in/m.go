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

func F(k Kind, n int) {
	switch k.(type) {
	case Book:
		n = 1
	case DVD:
		n = 2
	}
	println(n)
}
