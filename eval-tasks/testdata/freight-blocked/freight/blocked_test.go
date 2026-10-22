package freight

import (
	"reflect"
	"testing"
)

func TestAcceptBlocked(t *testing.T) {
	k, r := &Truck{}, &Train{}
	ss := []Shipment{
		{ID: "s1", Sender: "Ann", Road: "R1", Rail: "Y1", Blocked: map[string]bool{"train": true}},
		{ID: "s2", Sender: "Bo", Road: "R2", Rail: "Y2"},
	}
	got := Dispatch(Config{Carriers: []Carrier{k, r}}, ss, Order{Ref: "r", Weight: 1})
	if want := []string{"Ann: truck", "Bo: truck,train"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Dispatch = %q, want %q", got, want)
	}
	if want := []string{"Y2: r 1kg"}; !reflect.DeepEqual(r.Shipped, want) {
		t.Errorf("train shipped %q, want %q", r.Shipped, want)
	}
}
