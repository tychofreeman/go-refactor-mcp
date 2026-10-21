package m

type Message struct{}

type Batch struct {
	N       int
	Message Message
}

func Run(c int, b Batch) int { return c }

func local() int { return Run(1, Batch{N: 1, Message: Message{}}) }
