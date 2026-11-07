package m

type payload struct {
	val int
	tag string
}

type node struct {
	payload
	next *node
}

func pair() *node {
	return &node{payload: payload{val: 1, tag: "a"}, next: &node{payload: payload{val: 2, tag: "b"}}}
}

func list() []node { return []node{{payload: payload{val: 3}}, {payload: payload{tag: "c"}}} }
