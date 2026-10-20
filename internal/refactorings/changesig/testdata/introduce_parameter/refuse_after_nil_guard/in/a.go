package m

type node struct{ name string }

func label(n *node) string {
	if n == nil {
		return "<nil>"
	}
	return n.name
}

func Use(n *node) string { return label(n) }
