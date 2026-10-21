package notify

import (
	"reflect"
	"testing"
)

func TestAcceptContacts(t *testing.T) {
	e, s := &Email{}, &SMS{}
	u := User{ID: "u1", Name: "Ann", Contacts: map[string]string{"email": "a@x", "sms": "555"}}
	got := Broadcast(Config{Channels: []Notifier{e, s}}, []User{u}, Message{Subject: "A long subject for a text"})
	if want := []string{"Ann: email,sms"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"a@x: A long subject for a text"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q", e.Sent, want)
	}
	if want := []string{"555: A long subject for a"}; !reflect.DeepEqual(s.Sent, want) {
		t.Errorf("sms sent %q, want %q", s.Sent, want)
	}
}
