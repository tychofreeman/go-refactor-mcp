package m

type Email struct{ Sent []string }

func (e *Email) Send(to string) int {
	sent := &e.Sent
	*sent = append(*sent, to)
	return len(*sent)
}
