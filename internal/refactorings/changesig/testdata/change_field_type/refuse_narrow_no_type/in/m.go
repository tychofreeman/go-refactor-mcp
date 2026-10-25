package m

import "fmt"

type Kind interface{ Days() int }

type Item struct {
	Title string
	Kind  any
}

func Describe(it Item) string { return fmt.Sprint(it.Kind) }

func New(title string) Item { return Item{Title: title, Kind: "book"} }
