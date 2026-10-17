package m

import "fmt"

type list struct{ items []int }

type holder struct{ l list }

func run(h *holder) {
	l, v := &h.l, 3
	l.items = append(l.items, v)
	fmt.Println(h.l.items)
}
