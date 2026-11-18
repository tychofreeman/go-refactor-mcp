package notify

import (
	"reflect"
	"testing"
)

func TestBroadcastChannelsParameter(t *testing.T) {
	e := &Email{}
	got := Broadcast(Config{}, []User{{Name: "Ann", Email: "ann@x.org"}}, Message{Subject: "hi"}, []Notifier{e})
	if want := []string{"Ann: email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Broadcast = %q, want %q", got, want)
	}
	if want := []string{"ann@x.org: hi"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("email sent %q, want %q", e.Sent, want)
	}
}
