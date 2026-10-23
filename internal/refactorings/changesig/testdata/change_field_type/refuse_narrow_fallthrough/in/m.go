package m

import "fmt"

type Kind interface{ Days() int }

type Book struct{}

func (Book) Days() int { return 21 }

type Item struct {
	Title string
	Kind  any
}

func Days(it Item) int {
	switch it.Kind {
	case nil:
		fallthrough
	case "book":
		return 21
	}
	_ = fmt.Sprint(it.Kind)
	return 0
}
