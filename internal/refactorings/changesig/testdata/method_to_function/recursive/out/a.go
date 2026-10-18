package m

type node struct {
	next *node
	v    int
}

func total(n *node) int {
	if n == nil {
		return 0
	}
	return n.v + total(n.next)
}
