package m

type node struct{ next *node }

func (n *node) length() int {
	if n == nil {
		return 0
	}
	return 1 + n.next.length()
}

func Use() int { return (*node)(nil).length() }
