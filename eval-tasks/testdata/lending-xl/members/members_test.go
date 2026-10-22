package members

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

var ms = []lending.Member{
	{Name: "Bo", Street: "2 Oak", City: "Oslo", Zip: "0150"},
	{Name: "Ann", City: "oslo"},
	{Name: "Cy", Street: "9 Elm", City: "Bergen", Zip: "5003"},
}

func TestMembers(t *testing.T) {
	if got := Envelope(ms[0]); got != "Bo\n2 Oak\n0150 Oslo" {
		t.Errorf("Envelope = %q", got)
	}
	if got, want := Local(ms, "OSLO"), []string{"Ann", "Bo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Local = %q", got)
	}
	if got, want := MissingStreet(ms), []string{"Ann"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingStreet = %q", got)
	}
	if got := CityCounts(ms); got["Oslo"] != 1 || got["oslo"] != 1 || got["Bergen"] != 1 {
		t.Errorf("CityCounts = %v", got)
	}
	if !SameCity(ms[0], ms[1]) || Complete(ms[1]) || Area(ms[2]) != "50" || ZipLine(ms[1]) != "Ann: no zip" {
		t.Errorf("SameCity, Complete, Area or ZipLine")
	}
}
