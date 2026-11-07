package m

type node struct {
	val  int
	next *node
	tag  string
}

func pair() *node { return &node{val: 1, tag: "a", next: &node{val: 2, tag: "b"}} }

func list() []node { return []node{{val: 3}, {tag: "c"}} }
