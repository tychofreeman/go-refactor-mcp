package notify

import "testing"

func TestAcceptQuiet(t *testing.T) {
	e := &Email{}
	got := Broadcast(Config{Channels: []Notifier{e}, Quiet: true}, []User{{ID: "u1", Name: "Ann", Email: "a@x"}}, Message{Subject: "s"})
	if len(got) != 0 {
		t.Errorf("Quiet Broadcast returned %q, want nothing", got)
	}
	if len(e.Sent) != 1 {
		t.Errorf("Quiet Broadcast must still send; sent %q", e.Sent)
	}
}
