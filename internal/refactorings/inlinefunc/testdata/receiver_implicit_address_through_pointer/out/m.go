package m

import "fmt"

type list struct{ items []int }

type holder struct{ l list }

func run(h *holder) {
	l := &h.l
	l.items = append(l.items, 3)
	fmt.Println(h.l.items)
}
