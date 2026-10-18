package m

type node struct{ next *node }

func (n *node) after(k int) *node { return n.next }

func Use(n *node) *node { return n.after(0).after(0) }
