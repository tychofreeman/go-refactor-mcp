package audit

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

func TestAudit(t *testing.T) {
	ss := []freight.Shipment{{Sender: "Bo", Road: "nw-0042", Rail: "Y7"}, {Sender: "Cy", Rail: "Y9"}}
	if !Both(ss[0]) || Both(ss[1]) {
		t.Errorf("Both")
	}
	if got := Either(ss[1]); got != "Y9" {
		t.Errorf("Either = %q", got)
	}
	if got, want := Pairs(ss), []string{"nw-0042/Y7", "/Y9"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Pairs = %q", got)
	}
	if got := UpperRoad(ss[0]); got != "NW-0042" {
		t.Errorf("UpperRoad = %q", got)
	}
	if !SameRail(ss[0], freight.Shipment{Rail: " Y7 "}) {
		t.Errorf("SameRail")
	}
}
