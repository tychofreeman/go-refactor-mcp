package freight

import "testing"

func TestAcceptTail(t *testing.T) {
	c := &Truck{}
	if got := c.tail("12"); got != "12" {
		t.Errorf("tail(12) = %q", got)
	}
	if got := c.tail("RD-NORTH-0042"); got != "0042" {
		t.Errorf("tail cut to %q, want the last 4 bytes", got)
	}
	if got := c.Label(Shipment{Road: "RD-0004"}, Order{Ref: "R"}); got != "truck 0004 R" {
		t.Errorf("Label = %q", got)
	}
	if got := c.Tails([]Shipment{{Road: "1"}, {Road: "RD-0042"}}); len(got) != 2 || got[1] != "0042" {
		t.Errorf("Tails = %q", got)
	}
	if got := c.Badge(Shipment{Sender: "acme"}); got != "ACME" {
		t.Errorf("Badge = %q", got)
	}
}
