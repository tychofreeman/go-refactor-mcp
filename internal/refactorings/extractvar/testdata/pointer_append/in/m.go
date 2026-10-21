package m

type Email struct{ Sent []string }

func (e *Email) Send(to string) int {
	e.Sent = append(e.Sent, to)
	return len(e.Sent)
}
