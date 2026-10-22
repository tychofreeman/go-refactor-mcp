package depots

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

func TestDepots(t *testing.T) {
	p := &freight.Plane{}
	c := freight.Config{Carriers: []freight.Carrier{p}}
	ss := []freight.Shipment{{ID: "s1", Sender: "Ann", Road: "R1", Air: "LHR"}, {ID: "s2", Sender: "Bo", Air: "CDG"}}
	got := North(c, ss, freight.Order{Ref: "-7"})
	if want := []string{"Ann: plane"}; !reflect.DeepEqual(got, want) {
		t.Errorf("North = %q", got)
	}
	if want := []string{"LHR: N-7"}; !reflect.DeepEqual(p.Shipped, want) {
		t.Errorf("shipped %q, want %q", p.Shipped, want)
	}
}
