package m

type node struct{ next *node }

func length(n *node) int {
	if n == nil {
		return 0
	}
	return 1 + length(n.next)
}

func Use() int { return length(nil) }
