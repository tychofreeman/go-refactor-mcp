package notify

import (
	"reflect"
	"testing"
)

func TestAcceptPriority(t *testing.T) {
	if !(Low < Normal && Normal < High) {
		t.Errorf("want Low < Normal < High, got %d, %d, %d", Low, Normal, High)
	}
	e, s := &Email{}, &SMS{}
	u := []User{{Name: "Ann", Email: "a@x", Phone: "555"}}
	c := Config{Channels: []Notifier{e, s}}
	Broadcast(c, u, Message{Subject: "low"}, Low)
	Broadcast(c, u, Message{Subject: "high"}, High)
	e.Send(u[0], Message{Subject: "direct"}, Normal)
	if want := []string{"a@x: low", "a@x: high", "a@x: direct"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q", e.Sent, want)
	}
	if want := []string{"555: high"}; !reflect.DeepEqual(s.Sent, want) {
		t.Errorf("sms sent %q, want %q", s.Sent, want)
	}
}
