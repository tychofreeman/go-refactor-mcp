package m

type node struct {
	next *node
	v    int
}

func (n *node) total() int {
	if n == nil {
		return 0
	}
	return n.v + n.next.total()
}
