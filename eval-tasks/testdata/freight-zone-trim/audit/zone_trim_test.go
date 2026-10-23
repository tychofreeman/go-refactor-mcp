package audit

import (
	"testing"

	"example.com/freight/freight"
)

func TestAcceptZoneTrim(t *testing.T) {
	s := freight.Shipment{Road: " nw-1", Rail: "  y7x ", Air: " osl-12", Sea: "\tber-40 "}
	if RoadZone(s) != "NW" || RailZone(s) != "Y7" || AirZone(s) != "OS" || SeaZone(s) != "BE" {
		t.Errorf("zones = %q %q %q %q", RoadZone(s), RailZone(s), AirZone(s), SeaZone(s))
	}
	if RoadZone(freight.Shipment{Road: " x "}) != "" || SeaZone(freight.Shipment{Sea: "  "}) != "" {
		t.Errorf("a short code after trimming has no zone")
	}
	if UpperRoad(s) != " NW-1" || AirLength(s) != 7 {
		t.Errorf("other checks changed")
	}
}
