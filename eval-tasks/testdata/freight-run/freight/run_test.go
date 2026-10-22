package freight

import (
	"reflect"
	"testing"
)

func TestAcceptRun(t *testing.T) {
	p := &Plane{}
	got := Dispatch(Config{Carriers: []Carrier{p}}, Run{Shipments: []Shipment{{ID: "s1", Sender: "Ann", Air: "LHR"}}, Order: Order{Ref: "r"}})
	if want := []string{"Ann: plane"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Dispatch = %q, want %q", got, want)
	}
	if want := []string{"LHR: r"}; !reflect.DeepEqual(p.Shipped, want) {
		t.Errorf("plane shipped %q, want %q", p.Shipped, want)
	}
}
