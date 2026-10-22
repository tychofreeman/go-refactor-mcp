package freight

import (
	"bytes"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAcceptDispatch(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	var shipped []string
	dispatch(&shipped, "cart", "dock", "ORD-9")
	if want := []string{"dock: ORD-9"}; !reflect.DeepEqual(shipped, want) {
		t.Errorf("dispatch appended %q, want %q", shipped, want)
	}
	if !strings.Contains(buf.String(), "cart ship to dock") {
		t.Errorf("dispatch logged %q", buf.String())
	}
	buf.Reset()
	cs := []Carrier{&Truck{}, &Train{}, &Plane{}, &Vessel{}, &Van{}, &Freighter{}, &Drone{}, &Barge{}, &Courier{}, &Post{}}
	s := Shipment{ID: "S-77", Sender: "Ann", Road: "RD-0042", Rail: "Y7", Air: "LHR", Sea: "RTM"}
	Dispatch(Config{Carriers: cs}, []Shipment{s}, Order{Ref: "Ord-2", Weight: 5})
	for _, want := range []string{"truck ship to 0042", "train ship to Y7", "plane ship to LHR", "vessel ship to RTM", "van ship to RD-0042", "freighter ship to Y7", "drone ship to LHR", "barge ship to RTM", "courier ship to Ann", "post ship to S-77"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("shipping logged %q, want %q in it", buf.String(), want)
		}
	}
	got := []string{}
	for _, c := range cs {
		got = append(got, shippedBy(c)...)
	}
	want := []string{"0042: Ord-2", "Y7: Ord-2 5kg", "LHR: Ord-2", "RTM: ord-2", "RD-0042: Ord-2/van", "Y7: ORD-2", "LHR: D-Ord-2", "RTM: Ord-2", "Ann: Ord-2", "S-77: 5kg Ord-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shipped %q, want %q", got, want)
	}
}

func shippedBy(c Carrier) []string {
	switch c := c.(type) {
	case *Truck:
		return c.Shipped
	case *Train:
		return c.Shipped
	case *Plane:
		return c.Shipped
	case *Vessel:
		return c.Shipped
	case *Van:
		return c.Shipped
	case *Freighter:
		return c.Shipped
	case *Drone:
		return c.Shipped
	case *Barge:
		return c.Shipped
	case *Courier:
		return c.Shipped
	case *Post:
		return c.Shipped
	}
	return nil
}
