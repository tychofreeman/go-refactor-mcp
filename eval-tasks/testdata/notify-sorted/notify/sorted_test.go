package notify

import (
	"reflect"
	"testing"
)

func TestAcceptSorted(t *testing.T) {
	users := []User{{ID: "3", Name: "Cy", Email: "c"}, {ID: "2", Name: "Ann", Email: "a2"}, {ID: "1", Name: "Ann", Email: "a1"}}
	e := &Email{}
	got := Broadcast(Config{Channels: []Notifier{e}}, users, Message{Subject: "s"})
	if want := []string{"Ann: email", "Ann: email", "Cy: email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"a1: s", "a2: s", "c: s"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q (by name, then ID)", e.Sent, want)
	}
	if users[0].ID != "3" || users[2].ID != "1" {
		t.Errorf("Broadcast changed the caller's slice: %v", users)
	}
}
