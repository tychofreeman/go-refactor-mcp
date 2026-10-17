package m

import "fmt"

type list struct{ items []int }

func (l *list) add(v int) { l.items = append(l.items, v) }

type holder struct{ l list }

func run(h *holder) {
	h.l.add(3)
	fmt.Println(h.l.items)
}
