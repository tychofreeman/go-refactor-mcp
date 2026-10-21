package notify

import (
	"reflect"
	"testing"
)

func TestAcceptBatch(t *testing.T) {
	e := &Email{}
	got := Broadcast(Config{Channels: []Notifier{e}}, Batch{Users: []User{{ID: "u1", Name: "Ann", Email: "a@x"}}, Message: Message{Subject: "s"}})
	if want := []string{"Ann: email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"a@x: s"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q", e.Sent, want)
	}
}
