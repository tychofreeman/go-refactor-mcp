package m

import "fmt"

type Kind interface{ Days(it Item) int }

type Book struct{}

func (Book) Days(Item) int { return 21 }

type Item struct {
	Title string
	Kind  Kind
}

func Days(it Item) int {
	if k, ok := it.Kind.(Kind); ok {
		return k.Days(it)
	}
	switch it.Kind {
	case nil:
		return 7
	}
	return 0
}

func Describe(it Item) string { return fmt.Sprint(it.Kind) }

func New(title string) Item { return Item{title, Book{}} }

func Set(it *Item, k Kind) { it.Kind = k }
