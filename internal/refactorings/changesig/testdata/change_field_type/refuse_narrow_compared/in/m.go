package m

import "fmt"

type Kind interface{ Days() int }

type Book struct{}

func (Book) Days() int { return 21 }

type Item struct {
	Title string
	Kind  any
}

func Old(it Item) bool { return it.Kind == "atlas" }

func Describe(it Item) string { return fmt.Sprint(it.Kind) }
