package notify

import (
	"bytes"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAcceptRecord(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	var sent []string
	record(&sent, "pigeon", "roof", "hello")
	if want := []string{"roof: hello"}; !reflect.DeepEqual(sent, want) {
		t.Errorf("record appended %q, want %q", sent, want)
	}
	if !strings.Contains(buf.String(), "pigeon to roof") {
		t.Errorf("record logged %q, want \"pigeon to roof\"", buf.String())
	}
	buf.Reset()
	e, s := &Email{}, &SMS{}
	Broadcast(Config{Channels: []Notifier{e, s}}, []User{{ID: "u1", Name: "Ann", Email: "a@x", Phone: "555"}}, Message{Subject: "A long subject for a text"})
	if !strings.Contains(buf.String(), "email to a@x") || !strings.Contains(buf.String(), "sms to 555") {
		t.Errorf("sending logged %q", buf.String())
	}
	if want := []string{"555: A long subject for a"}; !reflect.DeepEqual(s.Sent, want) {
		t.Errorf("sms sent %q, want %q", s.Sent, want)
	}
}
