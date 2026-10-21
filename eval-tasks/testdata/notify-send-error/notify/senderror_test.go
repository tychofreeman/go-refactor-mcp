package notify

import (
	"reflect"
	"strings"
	"testing"
)

func TestAcceptSendErrors(t *testing.T) {
	e, s := &Email{}, &SMS{}
	if err := e.Send(User{Name: "Bo"}, Message{Subject: "s"}); err == nil || !strings.Contains(err.Error(), "Bo") {
		t.Errorf("email to a user without an address: err = %v, want one naming Bo", err)
	}
	if len(e.Sent) != 0 {
		t.Errorf("a failed email was recorded as sent: %q", e.Sent)
	}
	if err := s.Send(User{Name: "Bo"}, Message{Subject: "s"}); err == nil || !strings.Contains(err.Error(), "Bo") {
		t.Errorf("text to a user without a phone: err = %v, want one naming Bo", err)
	}
	if err := e.Send(User{Name: "Ann", Email: "a@x"}, Message{Subject: "s"}); err != nil {
		t.Errorf("email to Ann: %v", err)
	}
	got := Broadcast(Config{Channels: []Notifier{e, s}}, []User{{Name: "Ann", Email: "a@x"}, {Name: "Cy", Email: "c@x", Phone: "5"}}, Message{Subject: "s"})
	if want := []string{"Ann: email,sms!", "Cy: email,sms"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
}
