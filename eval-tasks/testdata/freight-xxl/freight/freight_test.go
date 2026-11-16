package freight

import (
	"reflect"
	"testing"
)

func TestDispatch(t *testing.T) {
	k, r := &Truck{}, &Train{}
	ss := []Shipment{{ID: "s1", Sender: "Ann", Road: "RD-NORTH-0042", Rail: "YARD7"}}
	got := Dispatch(Config{Carriers: []Carrier{k, r}}, ss, Order{Ref: "ORD-1", Weight: 25})
	if want := []string{"Ann: truck,train"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Dispatch = %q, want %q", got, want)
	}
	if want := []string{"0042: ORD-1"}; !reflect.DeepEqual(k.Shipped, want) {
		t.Errorf("truck shipped %q, want %q", k.Shipped, want)
	}
	if want := []string{"YARD7: ORD-1 25kg"}; !reflect.DeepEqual(r.Shipped, want) {
		t.Errorf("train shipped %q, want %q", r.Shipped, want)
	}
}

func TestTruckTexts(t *testing.T) {
	c := &Truck{}
	if got := c.Masked("RD-NORTH-0042"); got != "****0042" {
		t.Errorf("Masked = %q", got)
	}
	if got := c.Hint(42); got != "0042" {
		t.Errorf("Hint = %q", got)
	}
	if got := c.Plate(123456); got != "TR3456" {
		t.Errorf("Plate = %q", got)
	}
}
