package m

type Email struct{ Sent []string }

func Send(e, other *Email, to string) {
	e.Sent = append(e.Sent, to)
	e = other
	e.Sent = append(e.Sent, to)
}
