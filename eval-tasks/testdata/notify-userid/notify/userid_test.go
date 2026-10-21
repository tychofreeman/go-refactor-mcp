package notify

import (
	"reflect"
	"testing"
)

func TestAcceptUserID(t *testing.T) {
	for id, want := range map[UserID]bool{"u1": true, "": false, "a b": false} {
		if got := id.Valid(); got != want {
			t.Errorf("UserID(%q).Valid() = %v, want %v", id, got, want)
		}
	}
	e := &Email{}
	users := []User{{ID: UserID("u1"), Name: "Ann", Email: "a@x"}, {ID: "a b", Name: "Bo", Email: "b@x"}}
	got := Broadcast(Config{Channels: []Notifier{e}}, users, Message{Subject: "s"})
	if want := []string{"Ann: email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"a@x: s"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q", e.Sent, want)
	}
}
