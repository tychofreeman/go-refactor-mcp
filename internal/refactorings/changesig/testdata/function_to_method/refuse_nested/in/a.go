package m

type node struct{ next *node }

func after(n *node) *node { return n.next }

func Use(n *node) *node { return after(after(n)) }
