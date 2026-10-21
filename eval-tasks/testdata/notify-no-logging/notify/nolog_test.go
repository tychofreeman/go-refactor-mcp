package notify

import (
	"bytes"
	"log"
	"os"
	"testing"
)

func TestAcceptNoLogging(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	Broadcast(Config{Channels: []Notifier{&Email{}, &SMS{}}}, []User{{Name: "Ann", Email: "a@x", Phone: "5"}}, Message{Subject: "s"})
	if buf.Len() != 0 {
		t.Errorf("sending logged %q", buf.String())
	}
}
