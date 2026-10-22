package schedules

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

func TestSchedules(t *testing.T) {
	p := &freight.Plane{}
	c := freight.Config{Carriers: []freight.Carrier{p}}
	ss := []freight.Shipment{{ID: "s1", Sender: "Ann", Air: "LHR"}}
	Daily(c, ss)
	Archive(c, ss)
	if want := []string{"LHR: DAY-1", "LHR: ARCH-15"}; !reflect.DeepEqual(p.Shipped, want) {
		t.Errorf("shipped %q, want %q", p.Shipped, want)
	}
}
