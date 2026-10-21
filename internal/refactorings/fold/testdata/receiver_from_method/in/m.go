package m

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type SMS struct{ Sent []string }

func (s *SMS) fit(text string) string { return truncate(text, 20) }

func (s *SMS) Send(subject string) {
	s.Sent = append(s.Sent, truncate(subject, 20))
}

func other(x string) string { return truncate(x, 20) }

func shorter(x string) string { return truncate(x, 10) }
