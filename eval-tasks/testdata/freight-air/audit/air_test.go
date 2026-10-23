package audit

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

func TestAcceptAir(t *testing.T) {
	ss := []freight.Shipment{{Sender: "Bo", Air: "osl-12"}, {Sender: "Cy", Road: "nw-1"}, {Sender: "Di", Air: "OSL-12"}}
	if got, want := LackingAir(ss), []string{"Cy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LackingAir = %q", got)
	}
	if got := AirZone(ss[0]); got != "OS" {
		t.Errorf("AirZone = %q", got)
	}
	if AirZone(freight.Shipment{Air: "x"}) != "" {
		t.Errorf("AirZone of a short code")
	}
	if !SameAir(ss[0], freight.Shipment{Air: " osl-12 "}) || SameAir(ss[0], ss[2]) {
		t.Errorf("SameAir")
	}
	if got := ByAir(ss); len(got) != 3 || got["osl-12"].Sender != "Bo" || got[""].Sender != "Cy" {
		t.Errorf("ByAir = %v", got)
	}
	if got, want := AirCounts(ss), map[string]int{"osl-12": 1, "OSL-12": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("AirCounts = %v", got)
	}
	if UpperAir(ss[0]) != "OSL-12" || AirLength(ss[0]) != 6 {
		t.Errorf("UpperAir or AirLength")
	}
	if AirLine(ss[0]) != "Bo: osl-12" || AirLine(ss[1]) != "Cy: no air" {
		t.Errorf("AirLine = %q, %q", AirLine(ss[0]), AirLine(ss[1]))
	}
	if RoadLine(ss[1]) != "Cy: nw-1" || RailZone(freight.Shipment{Rail: "y7x"}) != "Y7" {
		t.Errorf("road or rail changed")
	}
}
