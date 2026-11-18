package notify

import (
	"reflect"
	"testing"
)

func TestBroadcastArgs(t *testing.T) {
	e := &Email{}
	got := Broadcast(BroadcastArgs{c: Config{Channels: []Notifier{e}}, users: []User{{Name: "Ann", Email: "ann@x.org"}}, m: Message{Subject: "hi"}})
	if want := []string{"Ann: email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
}
