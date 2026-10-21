package m

import "log"

type Email struct{ Sent []string }

type SMS struct{ Sent []string }

func record(sent *[]string, channel, to, text string) {
	log.Printf("%s to %s", channel, to)
	*sent = append(*sent, to+": "+text)
}

func (e *Email) Send(addr, subject string) {
	sent := &e.Sent
	record(sent, "email", addr, subject)
}

func (s *SMS) Send(phone, subject string) {
	sent := &s.Sent
	text := subject + "!"
	record(sent, "sms", phone, text)
}
