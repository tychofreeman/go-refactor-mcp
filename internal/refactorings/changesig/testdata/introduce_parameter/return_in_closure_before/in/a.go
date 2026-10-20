package m

type node struct{ name string }

func label(n *node) string {
	f := func() int { return 1 }
	_ = f
	_ = 0
	return n.name
}

func Use(n *node) string { return label(n) }
