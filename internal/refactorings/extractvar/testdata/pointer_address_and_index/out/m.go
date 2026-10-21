package m

type List struct{ items []int }

func grow(s *[]int) { *s = append(*s, 0) }

func (l *List) Reset() int {
	xs := &l.items
	grow(xs)
	(*xs)[0] = 1
	return (*xs)[0] + len(*xs)
}
