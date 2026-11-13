package m

import "fmt"

type order struct{ id int; open bool }

func ship(orders []order) {
outer:
	for _, o := range orders {
		if o.open {
			continue outer
		}
		fmt.Println(o.id)
	}
}
