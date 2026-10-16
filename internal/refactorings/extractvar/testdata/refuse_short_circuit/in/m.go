package m

type Node struct{ size int }

func Big(p *Node) bool {
	return p != nil && p.size > 10
}
