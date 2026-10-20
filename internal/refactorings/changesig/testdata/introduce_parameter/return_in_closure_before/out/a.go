package m

type node struct{ name string }

func label(n *node, name string) string {
	f := func() int { return 1 }
	_ = f
	_ = 0
	return name
}

func Use(n *node) string { return label(n, n.name) }
