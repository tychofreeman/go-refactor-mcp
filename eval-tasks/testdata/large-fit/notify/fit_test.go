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
	if got := s.Reminder(User{Phone: "5"}, "pay the bill today"); got != "5: Reminder: pay the bi" {
		t.Errorf("Reminder = %q", got)
	}
	if got := s.Digest([]Message{{Subject: "one"}, {Subject: "a subject that is longer than twenty"}}); len(got) != 2 || got[1] != "a subject that is lo" {
		t.Errorf("Digest = %q", got)
	}
}
