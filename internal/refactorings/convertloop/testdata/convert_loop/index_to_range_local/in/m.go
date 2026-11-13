package m

import "fmt"

type order struct{ id int; open bool }

func ship(orders []order) {
outer:
	for i := 0; i < len(orders); i += 1 {
		o := orders[i]
		if o.open {
			continue outer
		}
		fmt.Println(o.id)
	}
}
