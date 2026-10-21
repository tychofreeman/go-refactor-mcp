package report

import (
	"reflect"
	"testing"

	"example.com/notify/notify"
)

var users = []notify.User{
	{ID: "2", Name: "Bo", Email: "bo@y.com", Phone: "(555) 01"},
	{ID: "1", Name: "Ann", Email: "ann@x.org"},
	{ID: "3", Name: "Cy", Phone: "777"},
}

func TestReport(t *testing.T) {
	if got, want := Directory(users[:1]), []string{"Bo <bo@y.com> (555) 01"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Directory = %q, want %q", got, want)
	}
	if got, want := MissingEmail(users), []string{"Cy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingEmail = %q", got)
	}
	if got, want := MissingPhone(users), []string{"Ann"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingPhone = %q", got)
	}
	if got := PhoneDigits(users[0]); got != "55501" {
		t.Errorf("PhoneDigits = %q", got)
	}
	if got := CSV(users); got != "1,Ann,ann@x.org,\n2,Bo,bo@y.com,(555) 01\n3,Cy,,777\n" {
		t.Errorf("CSV = %q", got)
	}
	if got := Label(users[2]); got != "Cy (777)" {
		t.Errorf("Label = %q", got)
	}
	if got := Domains(users); got["x.org"] != 1 || got["y.com"] != 1 {
		t.Errorf("Domains = %v", got)
	}
}
