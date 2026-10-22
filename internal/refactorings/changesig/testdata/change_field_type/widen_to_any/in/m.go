package m

import "fmt"

type Item struct{ Kind string }

func Days(it Item) int {
	switch it.Kind {
	case "book":
		return 21
	case "dvd":
		return 7
	}
	return 0
}

func Fragile(it Item) bool { return it.Kind == "atlas" }

func Describe(it Item) string { return fmt.Sprintf("%q", it.Kind) }

func New(k string) Item { return Item{Kind: k} }
