package audit

import (
	"testing"

	"example.com/freight/freight"
)

func TestAcceptValid(t *testing.T) {
	ok := freight.Shipment{Road: "nw-1", Rail: "y7x", Air: "osl-12", Sea: "ber-40"}
	if !RoadValid(ok) || !RailValid(ok) || !AirValid(ok) || !SeaValid(ok) {
		t.Errorf("valid codes refused")
	}
	for _, code := range []string{"", "NW-1", "nw 1", "nw_1", " nw-1"} {
		bad := freight.Shipment{Road: code, Rail: code, Air: code, Sea: code}
		if RoadValid(bad) || RailValid(bad) || AirValid(bad) || SeaValid(bad) {
			t.Errorf("code %q accepted", code)
		}
	}
}
