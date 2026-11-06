package m

import "fmt"

// Priced has a price and a label.
type Priced interface {
	Price() int
	Label(prefix string) (fmt.Stringer, error)
}
