package audit

import (
	"testing"

	"example.com/freight/freight"
)

func TestAcceptLineTrim(t *testing.T) {
	s := freight.Shipment{Sender: "Cy", Road: " nw-1 ", Rail: "y7x ", Air: " osl-12", Sea: "  "}
	if RoadLine(s) != "Cy: nw-1" || RailLine(s) != "Cy: y7x" || AirLine(s) != "Cy: osl-12" || SeaLine(s) != "Cy: no sea" {
		t.Errorf("lines = %q %q %q %q", RoadLine(s), RailLine(s), AirLine(s), SeaLine(s))
	}
	if RoadLine(freight.Shipment{Sender: "Bo"}) != "Bo: no road" || RoadZone(s) != "NW" {
		t.Errorf("other checks changed")
	}
}
