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
	cs := []Carrier{&Truck{}, &Train{}, &Plane{}, &Vessel{}, &Van{}, &Freighter{}, &Drone{}, &Barge{}, &Courier{}, &Post{}, &Truck2{}, &Train2{}, &Plane2{}, &Vessel2{}, &Van2{}, &Freighter2{}, &Drone2{}, &Barge2{}, &Courier2{}, &Post2{}, &Truck3{}, &Train3{}, &Plane3{}, &Vessel3{}, &Van3{}, &Freighter3{}, &Drone3{}, &Barge3{}, &Courier3{}, &Post3{}, &Truck4{}, &Train4{}, &Plane4{}, &Vessel4{}, &Van4{}, &Freighter4{}, &Drone4{}, &Barge4{}, &Courier4{}, &Post4{}, &Truck5{}, &Train5{}, &Plane5{}, &Vessel5{}, &Van5{}, &Freighter5{}, &Drone5{}, &Barge5{}, &Courier5{}, &Post5{}}
	s := Shipment{ID: "S-77", Sender: "Ann", Road: "RD-0042", Rail: "Y7", Air: "LHR", Sea: "RTM"}
	Dispatch(Config{Carriers: cs}, []Shipment{s}, Order{Ref: "Ord-2", Weight: 5})
	for _, want := range []string{"truck ship to 0042", "train ship to Y7", "plane ship to LHR", "vessel ship to RTM", "van ship to RD-0042", "freighter ship to Y7", "drone ship to LHR", "barge ship to RTM", "courier ship to Ann", "post ship to S-77", "truck2 ship to RD-0042", "train2 ship to Y7", "plane2 ship to LHR", "vessel2 ship to RTM", "van2 ship to RD-0042", "freighter2 ship to Y7", "drone2 ship to LHR", "barge2 ship to RTM", "courier2 ship to Ann", "post2 ship to S-77", "truck3 ship to RD-0042", "train3 ship to Y7", "plane3 ship to LHR", "vessel3 ship to RTM", "van3 ship to RD-0042", "freighter3 ship to Y7", "drone3 ship to LHR", "barge3 ship to RTM", "courier3 ship to Ann", "post3 ship to S-77", "truck4 ship to RD-0042", "train4 ship to Y7", "plane4 ship to LHR", "vessel4 ship to RTM", "van4 ship to RD-0042", "freighter4 ship to Y7", "drone4 ship to LHR", "barge4 ship to RTM", "courier4 ship to Ann", "post4 ship to S-77", "truck5 ship to RD-0042", "train5 ship to Y7", "plane5 ship to LHR", "vessel5 ship to RTM", "van5 ship to RD-0042", "freighter5 ship to Y7", "drone5 ship to LHR", "barge5 ship to RTM", "courier5 ship to Ann", "post5 ship to S-77"} {
		if !strings.Contains(buf.String(), want+"\n") {
			t.Errorf("shipping logged %q, want %q in it", buf.String(), want)
		}
	}
	got := []string{}
	for _, c := range cs {
		got = append(got, shippedBy(c)...)
	}
	want := []string{"0042: Ord-2", "Y7: Ord-2 5kg", "LHR: Ord-2", "RTM: ord-2", "RD-0042: Ord-2/van", "Y7: ORD-2", "LHR: D-Ord-2", "RTM: Ord-2", "Ann: Ord-2", "S-77: 5kg Ord-2", "RD-0042: Ord-2", "Y7: Ord-2 5kg", "LHR: Ord-2", "RTM: ord-2", "RD-0042: Ord-2/van", "Y7: ORD-2", "LHR: D-Ord-2", "RTM: Ord-2", "Ann: Ord-2", "S-77: 5kg Ord-2", "RD-0042: Ord-2", "Y7: Ord-2 5kg", "LHR: Ord-2", "RTM: ord-2", "RD-0042: Ord-2/van", "Y7: ORD-2", "LHR: D-Ord-2", "RTM: Ord-2", "Ann: Ord-2", "S-77: 5kg Ord-2", "RD-0042: Ord-2", "Y7: Ord-2 5kg", "LHR: Ord-2", "RTM: ord-2", "RD-0042: Ord-2/van", "Y7: ORD-2", "LHR: D-Ord-2", "RTM: Ord-2", "Ann: Ord-2", "S-77: 5kg Ord-2", "RD-0042: Ord-2", "Y7: Ord-2 5kg", "LHR: Ord-2", "RTM: ord-2", "RD-0042: Ord-2/van", "Y7: ORD-2", "LHR: D-Ord-2", "RTM: Ord-2", "Ann: Ord-2", "S-77: 5kg Ord-2"}
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
	case *Truck2:
		return c.Shipped
	case *Train2:
		return c.Shipped
	case *Plane2:
		return c.Shipped
	case *Vessel2:
		return c.Shipped
	case *Van2:
		return c.Shipped
	case *Freighter2:
		return c.Shipped
	case *Drone2:
		return c.Shipped
	case *Barge2:
		return c.Shipped
	case *Courier2:
		return c.Shipped
	case *Post2:
		return c.Shipped
	case *Truck3:
		return c.Shipped
	case *Train3:
		return c.Shipped
	case *Plane3:
		return c.Shipped
	case *Vessel3:
		return c.Shipped
	case *Van3:
		return c.Shipped
	case *Freighter3:
		return c.Shipped
	case *Drone3:
		return c.Shipped
	case *Barge3:
		return c.Shipped
	case *Courier3:
		return c.Shipped
	case *Post3:
		return c.Shipped
	case *Truck4:
		return c.Shipped
	case *Train4:
		return c.Shipped
	case *Plane4:
		return c.Shipped
	case *Vessel4:
		return c.Shipped
	case *Van4:
		return c.Shipped
	case *Freighter4:
		return c.Shipped
	case *Drone4:
		return c.Shipped
	case *Barge4:
		return c.Shipped
	case *Courier4:
		return c.Shipped
	case *Post4:
		return c.Shipped
	case *Truck5:
		return c.Shipped
	case *Train5:
		return c.Shipped
	case *Plane5:
		return c.Shipped
	case *Vessel5:
		return c.Shipped
	case *Van5:
		return c.Shipped
	case *Freighter5:
		return c.Shipped
	case *Drone5:
		return c.Shipped
	case *Barge5:
		return c.Shipped
	case *Courier5:
		return c.Shipped
	case *Post5:
		return c.Shipped
	}
	return nil
}
