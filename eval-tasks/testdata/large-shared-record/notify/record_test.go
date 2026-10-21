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
		t.Errorf("record logged %q", buf.String())
	}
	buf.Reset()
	sl, p := &Slack{}, &Push{}
	Broadcast(Config{Channels: []Notifier{&Email{}, &SMS{}, sl, p}}, []User{{Name: "Ann", Email: "a", Phone: "5", Handle: "@ann", Token: "t1"}}, Message{Subject: "Hi"})
	for _, s := range []string{"email to a", "sms to 5", "slack to @ann", "push to t1"} {
		if !strings.Contains(buf.String(), s) {
			t.Errorf("sending logged %q, want %q in it", buf.String(), s)
		}
	}
	if !reflect.DeepEqual(sl.Sent, []string{"@ann: Hi"}) || !reflect.DeepEqual(p.Sent, []string{"t1: HI"}) {
		t.Errorf("slack sent %q, push sent %q", sl.Sent, p.Sent)
	}
}
