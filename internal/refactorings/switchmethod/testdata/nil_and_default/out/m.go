package m

type Kind interface {
	Days() int
	Fee() int
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
	if k == nil {
		return -1
	}
	return k.Fee()
}

func (Book) Fee() int {
	return 10
}

func (DVD) Fee() int {
	return 30
}
