package campaigns

import (
	"reflect"
	"testing"

	"example.com/notify/notify"
)

func TestCampaigns(t *testing.T) {
	e := &notify.Email{}
	c := notify.Config{Channels: []notify.Notifier{e}}
	users := []notify.User{{ID: "u1", Name: "Ann", Email: "a@x"}}
	Welcome(c, users)
	Security(c, users)
	if want := []string{"a@x: Welcome news", "a@x: Security news"}; !reflect.DeepEqual(e.Sent, want) {
		t.Errorf("sent %q, want %q", e.Sent, want)
	}
}
