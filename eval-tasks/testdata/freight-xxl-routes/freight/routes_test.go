package freight

import (
	"reflect"
	"testing"
)

func TestAcceptRoutes(t *testing.T) {
	k, r := &Truck{}, &Train{}
	s := Shipment{ID: "s1", Sender: "Ann", Routes: map[string]string{"road": "RD-NORTH-0042", "rail": "YARD7"}}
	got := Dispatch(Config{Carriers: []Carrier{k, r}}, []Shipment{s}, Order{Ref: "ORD-1", Weight: 25})
	if want := []string{"Ann: truck,train"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Dispatch = %q, want %q", got, want)
	}
	if want := []string{"0042: ORD-1"}; !reflect.DeepEqual(k.Shipped, want) {
		t.Errorf("truck shipped %q, want %q", k.Shipped, want)
	}
	if want := []string{"YARD7: ORD-1 25kg"}; !reflect.DeepEqual(r.Shipped, want) {
		t.Errorf("train shipped %q, want %q", r.Shipped, want)
	}
	if got := k.Label(s, Order{Ref: "R"}); got != "truck 0042 R" {
		t.Errorf("Label = %q", got)
	}
}
