package p

// Batch is a message and who to send it to.
type Batch struct {
	Users   []string
	Message string
}

func Send(users []string, m string) int { return len(users) + len(m) }

func Send2(b Batch) int { return Send(b.Users, b.Message) }
