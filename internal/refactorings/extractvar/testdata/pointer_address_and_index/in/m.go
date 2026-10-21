package m

type List struct{ items []int }

func grow(s *[]int) { *s = append(*s, 0) }

func (l *List) Reset() int {
	grow(&l.items)
	l.items[0] = 1
	return l.items[0] + len(l.items)
}
