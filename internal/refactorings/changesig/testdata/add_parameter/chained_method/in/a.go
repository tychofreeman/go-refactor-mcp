package m

type node struct{ next *node }

func (n *node) after() *node { return n.next }

func Use(n *node) *node { return n.after().after() }
