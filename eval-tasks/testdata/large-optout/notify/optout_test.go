package notify

import (
	"reflect"
	"testing"
)

func TestAcceptOptOut(t *testing.T) {
	e, s := &Email{}, &SMS{}
	users := []User{
		{ID: "u1", Name: "Ann", Email: "a@x", Phone: "5", OptOut: map[string]bool{"sms": true}},
		{ID: "u2", Name: "Bo", Email: "b@x", Phone: "6"},
	}
	got := Broadcast(Config{Channels: []Notifier{e, s}}, users, Message{Subject: "s"})
	if want := []string{"Ann: email", "Bo: email,sms"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"6: s"}; !reflect.DeepEqual(s.Sent, want) {
		t.Errorf("sms sent %q, want %q", s.Sent, want)
	}
}
