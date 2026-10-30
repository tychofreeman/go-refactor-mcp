package m

type Node struct{ Kids []*Node }

// walk calls f for n and each node under it, depth first.
func walk(n *Node, f func(*Node) bool) {
	var visit func(*Node)
	visit = func(n *Node) {
		if f(n) {
			for _, k := range n.Kids {
				visit(k)
			}
		}
	}
	visit(n)
}

func count(root *Node) int {
	n := size(root)
	return n
}

func size(root *Node) int {
	n := 0
	walk(root, func(*Node) bool {
		n++
		return true
	})
	return n
}
