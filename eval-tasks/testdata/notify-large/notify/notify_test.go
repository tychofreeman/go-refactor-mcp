package notify

import (
	"reflect"
	"testing"
)

func TestBroadcast(t *testing.T) {
	e, s := &Email{}, &SMS{}
	users := []User{{ID: "u1", Name: "Ann", Email: "ann@x.org", Phone: "555"}}
	got := Broadcast(Config{Channels: []Notifier{e, s}}, users, Message{Subject: "A long subject for a text"})
	if want := []string{"Ann: email,sms"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"ann@x.org: A long subject for a text"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q", e.Sent, want)
	}
	if want := []string{"555: A long subject for a"}; !reflect.DeepEqual(s.Sent, want) {
		t.Errorf("sms sent %q, want %q", s.Sent, want)
	}
}

func TestSMSTexts(t *testing.T) {
	s := &SMS{}
	if got := s.Preview(Message{Subject: "A long subject for a text"}); got != "A long subject for a" {
		t.Errorf("Preview = %q", got)
	}
	if got := s.Alert(7); got != "ALERT 7: act now" {
		t.Errorf("Alert = %q", got)
	}
}
