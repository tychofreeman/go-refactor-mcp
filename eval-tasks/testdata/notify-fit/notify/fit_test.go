package notify

import "testing"

func TestAcceptFit(t *testing.T) {
	s := &SMS{}
	if got := s.fit("short"); got != "short" {
		t.Errorf("fit(short) = %q", got)
	}
	if got := s.fit("a subject that is longer than twenty"); got != "a subject that is lo" {
		t.Errorf("fit cut to %q, want 20 bytes", got)
	}
	s.Send(User{Phone: "5"}, Message{Subject: "a subject that is longer than twenty"})
	if len(s.Sent) != 1 || s.Sent[0] != "5: a subject that is lo" {
		t.Errorf("sms sent %q", s.Sent)
	}
}
