package m

import "fmt"

// Bed is a bed of plants.
type Bed interface {
	fmt.Stringer
	Depth() int
}
