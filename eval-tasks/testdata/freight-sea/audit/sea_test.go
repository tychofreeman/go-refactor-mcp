package audit

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

func TestAcceptSea(t *testing.T) {
	ss := []freight.Shipment{{Sender: "Bo", Sea: "ber-40"}, {Sender: "Cy", Road: "nw-1"}, {Sender: "Di", Sea: "BER-40"}}
	if got, want := LackingSea(ss), []string{"Cy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LackingSea = %q", got)
	}
	if got := SeaZone(ss[0]); got != "BE" {
		t.Errorf("SeaZone = %q", got)
	}
	if SeaZone(freight.Shipment{Sea: "x"}) != "" {
		t.Errorf("SeaZone of a short code")
	}
	if !SameSea(ss[0], freight.Shipment{Sea: " ber-40 "}) || SameSea(ss[0], ss[2]) {
		t.Errorf("SameSea")
	}
	if got := BySea(ss); len(got) != 3 || got["ber-40"].Sender != "Bo" || got[""].Sender != "Cy" {
		t.Errorf("BySea = %v", got)
	}
	if got, want := SeaCounts(ss), map[string]int{"ber-40": 1, "BER-40": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("SeaCounts = %v", got)
	}
	if UpperSea(ss[0]) != "BER-40" || SeaLength(ss[0]) != 6 {
		t.Errorf("UpperSea or SeaLength")
	}
	if SeaLine(ss[0]) != "Bo: ber-40" || SeaLine(ss[1]) != "Cy: no sea" {
		t.Errorf("SeaLine = %q, %q", SeaLine(ss[0]), SeaLine(ss[1]))
	}
	if AirLine(freight.Shipment{Sender: "Ed", Air: "osl-12"}) != "Ed: osl-12" || RoadLine(ss[1]) != "Cy: nw-1" || RailZone(freight.Shipment{Rail: "y7x"}) != "Y7" {
		t.Errorf("road, rail or air changed")
	}
}
