// Package freight ships orders through several carriers.
package freight

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// Shipment is something to send, with where each carrier picks it up.
type Shipment struct {
	ID     string
	Sender string
	Road   string // a road depot code
	Rail   string // a rail yard code
	Air    string // an airport code
	Sea    string // a port code
}

// Order is what is shipped.
type Order struct {
	Ref    string
	Weight int // in kilograms
}

// Carrier ships an order to a shipment's pickup point by one means.
type Carrier interface {
	Ship(s Shipment, o Order)
	Name() string
}

// Truck ships by truck.
type Truck struct {
	Shipped []string // what was shipped, "truck: ref"
}

// Ship sends o to s by truck.
func (c *Truck) Ship(s Shipment, o Order) {
	log.Printf("truck ship to %s", last(s.Road, 4))
	c.Shipped = append(c.Shipped, last(s.Road, 4)+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Truck) Name() string { return "truck" }

// Train ships by train.
type Train struct {
	Shipped []string // what was shipped, "train: ref"
}

// Ship sends o to s by train.
func (c *Train) Ship(s Shipment, o Order) {
	log.Printf("train ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+o.Ref+fmt.Sprintf(" %dkg", o.Weight))
}

// Name is the carrier's name.
func (c *Train) Name() string { return "train" }

// Plane ships by plane.
type Plane struct {
	Shipped []string // what was shipped, "plane: ref"
}

// Ship sends o to s by plane.
func (c *Plane) Ship(s Shipment, o Order) {
	log.Printf("plane ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Plane) Name() string { return "plane" }

// Vessel ships by vessel.
type Vessel struct {
	Shipped []string // what was shipped, "vessel: ref"
}

// Ship sends o to s by vessel.
func (c *Vessel) Ship(s Shipment, o Order) {
	log.Printf("vessel ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+strings.ToLower(o.Ref))
}

// Name is the carrier's name.
func (c *Vessel) Name() string { return "vessel" }

// Van ships by van.
type Van struct {
	Shipped []string // what was shipped, "van: ref"
}

// Ship sends o to s by van.
func (c *Van) Ship(s Shipment, o Order) {
	log.Printf("van ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref+"/van")
}

// Name is the carrier's name.
func (c *Van) Name() string { return "van" }

// Freighter ships by freighter.
type Freighter struct {
	Shipped []string // what was shipped, "freighter: ref"
}

// Ship sends o to s by freighter.
func (c *Freighter) Ship(s Shipment, o Order) {
	log.Printf("freighter ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+strings.ToUpper(o.Ref))
}

// Name is the carrier's name.
func (c *Freighter) Name() string { return "freighter" }

// Drone ships by drone.
type Drone struct {
	Shipped []string // what was shipped, "drone: ref"
}

// Ship sends o to s by drone.
func (c *Drone) Ship(s Shipment, o Order) {
	log.Printf("drone ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+"D-"+o.Ref)
}

// Name is the carrier's name.
func (c *Drone) Name() string { return "drone" }

// Barge ships by barge.
type Barge struct {
	Shipped []string // what was shipped, "barge: ref"
}

// Ship sends o to s by barge.
func (c *Barge) Ship(s Shipment, o Order) {
	log.Printf("barge ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Barge) Name() string { return "barge" }

// Courier ships by courier.
type Courier struct {
	Shipped []string // what was shipped, "courier: ref"
}

// Ship sends o to s by courier.
func (c *Courier) Ship(s Shipment, o Order) {
	log.Printf("courier ship to %s", s.Sender)
	c.Shipped = append(c.Shipped, s.Sender+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Courier) Name() string { return "courier" }

// Post ships by post.
type Post struct {
	Shipped []string // what was shipped, "post: ref"
}

// Ship sends o to s by post.
func (c *Post) Ship(s Shipment, o Order) {
	log.Printf("post ship to %s", s.ID)
	c.Shipped = append(c.Shipped, s.ID+": "+fmt.Sprint(o.Weight)+"kg "+o.Ref)
}

// Name is the carrier's name.
func (c *Post) Name() string { return "post" }

// Truck2 ships by truck2.
type Truck2 struct {
	Shipped []string // what was shipped, "truck2: ref"
}

// Ship sends o to s by truck2.
func (c *Truck2) Ship(s Shipment, o Order) {
	log.Printf("truck2 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Truck2) Name() string { return "truck2" }

// Train2 ships by train2.
type Train2 struct {
	Shipped []string // what was shipped, "train2: ref"
}

// Ship sends o to s by train2.
func (c *Train2) Ship(s Shipment, o Order) {
	log.Printf("train2 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+o.Ref+fmt.Sprintf(" %dkg", o.Weight))
}

// Name is the carrier's name.
func (c *Train2) Name() string { return "train2" }

// Plane2 ships by plane2.
type Plane2 struct {
	Shipped []string // what was shipped, "plane2: ref"
}

// Ship sends o to s by plane2.
func (c *Plane2) Ship(s Shipment, o Order) {
	log.Printf("plane2 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Plane2) Name() string { return "plane2" }

// Vessel2 ships by vessel2.
type Vessel2 struct {
	Shipped []string // what was shipped, "vessel2: ref"
}

// Ship sends o to s by vessel2.
func (c *Vessel2) Ship(s Shipment, o Order) {
	log.Printf("vessel2 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+strings.ToLower(o.Ref))
}

// Name is the carrier's name.
func (c *Vessel2) Name() string { return "vessel2" }

// Van2 ships by van2.
type Van2 struct {
	Shipped []string // what was shipped, "van2: ref"
}

// Ship sends o to s by van2.
func (c *Van2) Ship(s Shipment, o Order) {
	log.Printf("van2 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref+"/van")
}

// Name is the carrier's name.
func (c *Van2) Name() string { return "van2" }

// Freighter2 ships by freighter2.
type Freighter2 struct {
	Shipped []string // what was shipped, "freighter2: ref"
}

// Ship sends o to s by freighter2.
func (c *Freighter2) Ship(s Shipment, o Order) {
	log.Printf("freighter2 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+strings.ToUpper(o.Ref))
}

// Name is the carrier's name.
func (c *Freighter2) Name() string { return "freighter2" }

// Drone2 ships by drone2.
type Drone2 struct {
	Shipped []string // what was shipped, "drone2: ref"
}

// Ship sends o to s by drone2.
func (c *Drone2) Ship(s Shipment, o Order) {
	log.Printf("drone2 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+"D-"+o.Ref)
}

// Name is the carrier's name.
func (c *Drone2) Name() string { return "drone2" }

// Barge2 ships by barge2.
type Barge2 struct {
	Shipped []string // what was shipped, "barge2: ref"
}

// Ship sends o to s by barge2.
func (c *Barge2) Ship(s Shipment, o Order) {
	log.Printf("barge2 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Barge2) Name() string { return "barge2" }

// Courier2 ships by courier2.
type Courier2 struct {
	Shipped []string // what was shipped, "courier2: ref"
}

// Ship sends o to s by courier2.
func (c *Courier2) Ship(s Shipment, o Order) {
	log.Printf("courier2 ship to %s", s.Sender)
	c.Shipped = append(c.Shipped, s.Sender+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Courier2) Name() string { return "courier2" }

// Post2 ships by post2.
type Post2 struct {
	Shipped []string // what was shipped, "post2: ref"
}

// Ship sends o to s by post2.
func (c *Post2) Ship(s Shipment, o Order) {
	log.Printf("post2 ship to %s", s.ID)
	c.Shipped = append(c.Shipped, s.ID+": "+fmt.Sprint(o.Weight)+"kg "+o.Ref)
}

// Name is the carrier's name.
func (c *Post2) Name() string { return "post2" }

// Truck3 ships by truck3.
type Truck3 struct {
	Shipped []string // what was shipped, "truck3: ref"
}

// Ship sends o to s by truck3.
func (c *Truck3) Ship(s Shipment, o Order) {
	log.Printf("truck3 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Truck3) Name() string { return "truck3" }

// Train3 ships by train3.
type Train3 struct {
	Shipped []string // what was shipped, "train3: ref"
}

// Ship sends o to s by train3.
func (c *Train3) Ship(s Shipment, o Order) {
	log.Printf("train3 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+o.Ref+fmt.Sprintf(" %dkg", o.Weight))
}

// Name is the carrier's name.
func (c *Train3) Name() string { return "train3" }

// Plane3 ships by plane3.
type Plane3 struct {
	Shipped []string // what was shipped, "plane3: ref"
}

// Ship sends o to s by plane3.
func (c *Plane3) Ship(s Shipment, o Order) {
	log.Printf("plane3 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Plane3) Name() string { return "plane3" }

// Vessel3 ships by vessel3.
type Vessel3 struct {
	Shipped []string // what was shipped, "vessel3: ref"
}

// Ship sends o to s by vessel3.
func (c *Vessel3) Ship(s Shipment, o Order) {
	log.Printf("vessel3 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+strings.ToLower(o.Ref))
}

// Name is the carrier's name.
func (c *Vessel3) Name() string { return "vessel3" }

// Van3 ships by van3.
type Van3 struct {
	Shipped []string // what was shipped, "van3: ref"
}

// Ship sends o to s by van3.
func (c *Van3) Ship(s Shipment, o Order) {
	log.Printf("van3 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref+"/van")
}

// Name is the carrier's name.
func (c *Van3) Name() string { return "van3" }

// Freighter3 ships by freighter3.
type Freighter3 struct {
	Shipped []string // what was shipped, "freighter3: ref"
}

// Ship sends o to s by freighter3.
func (c *Freighter3) Ship(s Shipment, o Order) {
	log.Printf("freighter3 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+strings.ToUpper(o.Ref))
}

// Name is the carrier's name.
func (c *Freighter3) Name() string { return "freighter3" }

// Drone3 ships by drone3.
type Drone3 struct {
	Shipped []string // what was shipped, "drone3: ref"
}

// Ship sends o to s by drone3.
func (c *Drone3) Ship(s Shipment, o Order) {
	log.Printf("drone3 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+"D-"+o.Ref)
}

// Name is the carrier's name.
func (c *Drone3) Name() string { return "drone3" }

// Barge3 ships by barge3.
type Barge3 struct {
	Shipped []string // what was shipped, "barge3: ref"
}

// Ship sends o to s by barge3.
func (c *Barge3) Ship(s Shipment, o Order) {
	log.Printf("barge3 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Barge3) Name() string { return "barge3" }

// Courier3 ships by courier3.
type Courier3 struct {
	Shipped []string // what was shipped, "courier3: ref"
}

// Ship sends o to s by courier3.
func (c *Courier3) Ship(s Shipment, o Order) {
	log.Printf("courier3 ship to %s", s.Sender)
	c.Shipped = append(c.Shipped, s.Sender+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Courier3) Name() string { return "courier3" }

// Post3 ships by post3.
type Post3 struct {
	Shipped []string // what was shipped, "post3: ref"
}

// Ship sends o to s by post3.
func (c *Post3) Ship(s Shipment, o Order) {
	log.Printf("post3 ship to %s", s.ID)
	c.Shipped = append(c.Shipped, s.ID+": "+fmt.Sprint(o.Weight)+"kg "+o.Ref)
}

// Name is the carrier's name.
func (c *Post3) Name() string { return "post3" }

// Truck4 ships by truck4.
type Truck4 struct {
	Shipped []string // what was shipped, "truck4: ref"
}

// Ship sends o to s by truck4.
func (c *Truck4) Ship(s Shipment, o Order) {
	log.Printf("truck4 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Truck4) Name() string { return "truck4" }

// Train4 ships by train4.
type Train4 struct {
	Shipped []string // what was shipped, "train4: ref"
}

// Ship sends o to s by train4.
func (c *Train4) Ship(s Shipment, o Order) {
	log.Printf("train4 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+o.Ref+fmt.Sprintf(" %dkg", o.Weight))
}

// Name is the carrier's name.
func (c *Train4) Name() string { return "train4" }

// Plane4 ships by plane4.
type Plane4 struct {
	Shipped []string // what was shipped, "plane4: ref"
}

// Ship sends o to s by plane4.
func (c *Plane4) Ship(s Shipment, o Order) {
	log.Printf("plane4 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Plane4) Name() string { return "plane4" }

// Vessel4 ships by vessel4.
type Vessel4 struct {
	Shipped []string // what was shipped, "vessel4: ref"
}

// Ship sends o to s by vessel4.
func (c *Vessel4) Ship(s Shipment, o Order) {
	log.Printf("vessel4 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+strings.ToLower(o.Ref))
}

// Name is the carrier's name.
func (c *Vessel4) Name() string { return "vessel4" }

// Van4 ships by van4.
type Van4 struct {
	Shipped []string // what was shipped, "van4: ref"
}

// Ship sends o to s by van4.
func (c *Van4) Ship(s Shipment, o Order) {
	log.Printf("van4 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref+"/van")
}

// Name is the carrier's name.
func (c *Van4) Name() string { return "van4" }

// Freighter4 ships by freighter4.
type Freighter4 struct {
	Shipped []string // what was shipped, "freighter4: ref"
}

// Ship sends o to s by freighter4.
func (c *Freighter4) Ship(s Shipment, o Order) {
	log.Printf("freighter4 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+strings.ToUpper(o.Ref))
}

// Name is the carrier's name.
func (c *Freighter4) Name() string { return "freighter4" }

// Drone4 ships by drone4.
type Drone4 struct {
	Shipped []string // what was shipped, "drone4: ref"
}

// Ship sends o to s by drone4.
func (c *Drone4) Ship(s Shipment, o Order) {
	log.Printf("drone4 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+"D-"+o.Ref)
}

// Name is the carrier's name.
func (c *Drone4) Name() string { return "drone4" }

// Barge4 ships by barge4.
type Barge4 struct {
	Shipped []string // what was shipped, "barge4: ref"
}

// Ship sends o to s by barge4.
func (c *Barge4) Ship(s Shipment, o Order) {
	log.Printf("barge4 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Barge4) Name() string { return "barge4" }

// Courier4 ships by courier4.
type Courier4 struct {
	Shipped []string // what was shipped, "courier4: ref"
}

// Ship sends o to s by courier4.
func (c *Courier4) Ship(s Shipment, o Order) {
	log.Printf("courier4 ship to %s", s.Sender)
	c.Shipped = append(c.Shipped, s.Sender+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Courier4) Name() string { return "courier4" }

// Post4 ships by post4.
type Post4 struct {
	Shipped []string // what was shipped, "post4: ref"
}

// Ship sends o to s by post4.
func (c *Post4) Ship(s Shipment, o Order) {
	log.Printf("post4 ship to %s", s.ID)
	c.Shipped = append(c.Shipped, s.ID+": "+fmt.Sprint(o.Weight)+"kg "+o.Ref)
}

// Name is the carrier's name.
func (c *Post4) Name() string { return "post4" }

// Truck5 ships by truck5.
type Truck5 struct {
	Shipped []string // what was shipped, "truck5: ref"
}

// Ship sends o to s by truck5.
func (c *Truck5) Ship(s Shipment, o Order) {
	log.Printf("truck5 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Truck5) Name() string { return "truck5" }

// Train5 ships by train5.
type Train5 struct {
	Shipped []string // what was shipped, "train5: ref"
}

// Ship sends o to s by train5.
func (c *Train5) Ship(s Shipment, o Order) {
	log.Printf("train5 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+o.Ref+fmt.Sprintf(" %dkg", o.Weight))
}

// Name is the carrier's name.
func (c *Train5) Name() string { return "train5" }

// Plane5 ships by plane5.
type Plane5 struct {
	Shipped []string // what was shipped, "plane5: ref"
}

// Ship sends o to s by plane5.
func (c *Plane5) Ship(s Shipment, o Order) {
	log.Printf("plane5 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Plane5) Name() string { return "plane5" }

// Vessel5 ships by vessel5.
type Vessel5 struct {
	Shipped []string // what was shipped, "vessel5: ref"
}

// Ship sends o to s by vessel5.
func (c *Vessel5) Ship(s Shipment, o Order) {
	log.Printf("vessel5 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+strings.ToLower(o.Ref))
}

// Name is the carrier's name.
func (c *Vessel5) Name() string { return "vessel5" }

// Van5 ships by van5.
type Van5 struct {
	Shipped []string // what was shipped, "van5: ref"
}

// Ship sends o to s by van5.
func (c *Van5) Ship(s Shipment, o Order) {
	log.Printf("van5 ship to %s", s.Road)
	c.Shipped = append(c.Shipped, s.Road+": "+o.Ref+"/van")
}

// Name is the carrier's name.
func (c *Van5) Name() string { return "van5" }

// Freighter5 ships by freighter5.
type Freighter5 struct {
	Shipped []string // what was shipped, "freighter5: ref"
}

// Ship sends o to s by freighter5.
func (c *Freighter5) Ship(s Shipment, o Order) {
	log.Printf("freighter5 ship to %s", s.Rail)
	c.Shipped = append(c.Shipped, s.Rail+": "+strings.ToUpper(o.Ref))
}

// Name is the carrier's name.
func (c *Freighter5) Name() string { return "freighter5" }

// Drone5 ships by drone5.
type Drone5 struct {
	Shipped []string // what was shipped, "drone5: ref"
}

// Ship sends o to s by drone5.
func (c *Drone5) Ship(s Shipment, o Order) {
	log.Printf("drone5 ship to %s", s.Air)
	c.Shipped = append(c.Shipped, s.Air+": "+"D-"+o.Ref)
}

// Name is the carrier's name.
func (c *Drone5) Name() string { return "drone5" }

// Barge5 ships by barge5.
type Barge5 struct {
	Shipped []string // what was shipped, "barge5: ref"
}

// Ship sends o to s by barge5.
func (c *Barge5) Ship(s Shipment, o Order) {
	log.Printf("barge5 ship to %s", s.Sea)
	c.Shipped = append(c.Shipped, s.Sea+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Barge5) Name() string { return "barge5" }

// Courier5 ships by courier5.
type Courier5 struct {
	Shipped []string // what was shipped, "courier5: ref"
}

// Ship sends o to s by courier5.
func (c *Courier5) Ship(s Shipment, o Order) {
	log.Printf("courier5 ship to %s", s.Sender)
	c.Shipped = append(c.Shipped, s.Sender+": "+o.Ref)
}

// Name is the carrier's name.
func (c *Courier5) Name() string { return "courier5" }

// Post5 ships by post5.
type Post5 struct {
	Shipped []string // what was shipped, "post5: ref"
}

// Ship sends o to s by post5.
func (c *Post5) Ship(s Shipment, o Order) {
	log.Printf("post5 ship to %s", s.ID)
	c.Shipped = append(c.Shipped, s.ID+": "+fmt.Sprint(o.Weight)+"kg "+o.Ref)
}

// Name is the carrier's name.
func (c *Post5) Name() string { return "post5" }

// Label is what a truck's label for o says.
func (c *Truck) Label(s Shipment, o Order) string {
	return "truck " + last(s.Road, 4) + " " + o.Ref
}

// Masked is a depot code as manifests show it.
func (c *Truck) Masked(code string) string { return "****" + last(code, 4) }

// Tails is the last 4 characters of each shipment's depot.
func (c *Truck) Tails(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, last(s.Road, 4))
	}
	return out
}

// Hint is a hint for a depot on file, from its number.
func (c *Truck) Hint(n int) string { return last(fmt.Sprintf("%08d", n), 4) }

// Short is a shipment's short form for drivers.
func (c *Truck) Short(s Shipment) string { return "T-" + last(s.ID, 4) }

// Track is the tracking suffix of o.
func (c *Truck) Track(o Order) string { return last(o.Ref, 4) }

// Badge is the sender's badge on the cab.
func (c *Truck) Badge(s Shipment) string { return strings.ToUpper(last(s.Sender, 4)) }

// Plate is a trailer plate from its serial number.
func (c *Truck) Plate(n int) string { return "TR" + last(fmt.Sprint(n), 4) }

// Same reports whether two shipments leave from the same depot, by the
// part a driver sees.
func (c *Truck) Same(a, b Shipment) bool { return last(a.Road, 4) == last(b.Road, 4) }

// Codes is the visible depot code of each shipment with a depot.
func (c *Truck) Codes(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Road != "" {
			out = append(out, last(s.Road, 4))
		}
	}
	return out
}

// Label2 is what a truck's label for o says.
func (c *Truck) Label2(s Shipment, o Order) string {
	return "truck " + last(s.Road, 4) + " " + o.Ref
}

// Masked2 is a depot code as manifests show it.
func (c *Truck) Masked2(code string) string { return "****" + last(code, 4) }

// Tails2 is the last 4 characters of each shipment's depot.
func (c *Truck) Tails2(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, last(s.Road, 4))
	}
	return out
}

// Hint2 is a hint for a depot on file, from its number.
func (c *Truck) Hint2(n int) string { return last(fmt.Sprintf("%08d", n), 4) }

// Short2 is a shipment's short form for drivers.
func (c *Truck) Short2(s Shipment) string { return "T-" + last(s.ID, 4) }

// Track2 is the tracking suffix of o.
func (c *Truck) Track2(o Order) string { return last(o.Ref, 4) }

// Badge2 is the sender's badge on the cab.
func (c *Truck) Badge2(s Shipment) string { return strings.ToUpper(last(s.Sender, 4)) }

// Plate2 is a trailer plate from its serial number.
func (c *Truck) Plate2(n int) string { return "TR" + last(fmt.Sprint(n), 4) }

// Same2 reports whether two shipments leave from the same depot, by the
// part a driver sees.
func (c *Truck) Same2(a, b Shipment) bool { return last(a.Road, 4) == last(b.Road, 4) }

// Codes2 is the visible depot code of each shipment with a depot.
func (c *Truck) Codes2(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Road != "" {
			out = append(out, last(s.Road, 4))
		}
	}
	return out
}

// Label3 is what a truck's label for o says.
func (c *Truck) Label3(s Shipment, o Order) string {
	return "truck " + last(s.Road, 4) + " " + o.Ref
}

// Masked3 is a depot code as manifests show it.
func (c *Truck) Masked3(code string) string { return "****" + last(code, 4) }

// Tails3 is the last 4 characters of each shipment's depot.
func (c *Truck) Tails3(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, last(s.Road, 4))
	}
	return out
}

// Hint3 is a hint for a depot on file, from its number.
func (c *Truck) Hint3(n int) string { return last(fmt.Sprintf("%08d", n), 4) }

// Short3 is a shipment's short form for drivers.
func (c *Truck) Short3(s Shipment) string { return "T-" + last(s.ID, 4) }

// Track3 is the tracking suffix of o.
func (c *Truck) Track3(o Order) string { return last(o.Ref, 4) }

// Badge3 is the sender's badge on the cab.
func (c *Truck) Badge3(s Shipment) string { return strings.ToUpper(last(s.Sender, 4)) }

// Plate3 is a trailer plate from its serial number.
func (c *Truck) Plate3(n int) string { return "TR" + last(fmt.Sprint(n), 4) }

// Same3 reports whether two shipments leave from the same depot, by the
// part a driver sees.
func (c *Truck) Same3(a, b Shipment) bool { return last(a.Road, 4) == last(b.Road, 4) }

// Codes3 is the visible depot code of each shipment with a depot.
func (c *Truck) Codes3(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Road != "" {
			out = append(out, last(s.Road, 4))
		}
	}
	return out
}

// Label4 is what a truck's label for o says.
func (c *Truck) Label4(s Shipment, o Order) string {
	return "truck " + last(s.Road, 4) + " " + o.Ref
}

// Masked4 is a depot code as manifests show it.
func (c *Truck) Masked4(code string) string { return "****" + last(code, 4) }

// Tails4 is the last 4 characters of each shipment's depot.
func (c *Truck) Tails4(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, last(s.Road, 4))
	}
	return out
}

// Hint4 is a hint for a depot on file, from its number.
func (c *Truck) Hint4(n int) string { return last(fmt.Sprintf("%08d", n), 4) }

// Short4 is a shipment's short form for drivers.
func (c *Truck) Short4(s Shipment) string { return "T-" + last(s.ID, 4) }

// Track4 is the tracking suffix of o.
func (c *Truck) Track4(o Order) string { return last(o.Ref, 4) }

// Badge4 is the sender's badge on the cab.
func (c *Truck) Badge4(s Shipment) string { return strings.ToUpper(last(s.Sender, 4)) }

// Plate4 is a trailer plate from its serial number.
func (c *Truck) Plate4(n int) string { return "TR" + last(fmt.Sprint(n), 4) }

// Same4 reports whether two shipments leave from the same depot, by the
// part a driver sees.
func (c *Truck) Same4(a, b Shipment) bool { return last(a.Road, 4) == last(b.Road, 4) }

// Codes4 is the visible depot code of each shipment with a depot.
func (c *Truck) Codes4(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Road != "" {
			out = append(out, last(s.Road, 4))
		}
	}
	return out
}

// Label5 is what a truck's label for o says.
func (c *Truck) Label5(s Shipment, o Order) string {
	return "truck " + last(s.Road, 4) + " " + o.Ref
}

// Masked5 is a depot code as manifests show it.
func (c *Truck) Masked5(code string) string { return "****" + last(code, 4) }

// Tails5 is the last 4 characters of each shipment's depot.
func (c *Truck) Tails5(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, last(s.Road, 4))
	}
	return out
}

// Hint5 is a hint for a depot on file, from its number.
func (c *Truck) Hint5(n int) string { return last(fmt.Sprintf("%08d", n), 4) }

// Short5 is a shipment's short form for drivers.
func (c *Truck) Short5(s Shipment) string { return "T-" + last(s.ID, 4) }

// Track5 is the tracking suffix of o.
func (c *Truck) Track5(o Order) string { return last(o.Ref, 4) }

// Badge5 is the sender's badge on the cab.
func (c *Truck) Badge5(s Shipment) string { return strings.ToUpper(last(s.Sender, 4)) }

// Plate5 is a trailer plate from its serial number.
func (c *Truck) Plate5(n int) string { return "TR" + last(fmt.Sprint(n), 4) }

// Same5 reports whether two shipments leave from the same depot, by the
// part a driver sees.
func (c *Truck) Same5(a, b Shipment) bool { return last(a.Road, 4) == last(b.Road, 4) }

// Codes5 is the visible depot code of each shipment with a depot.
func (c *Truck) Codes5(ss []Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Road != "" {
			out = append(out, last(s.Road, 4))
		}
	}
	return out
}

func last(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Config says how Dispatch ships.
type Config struct {
	Carriers []Carrier
}

// Dispatch ships o to each shipment through every carrier, in the order
// given, and returns a summary line for each: "sender: name,name".
func Dispatch(c Config, shipments []Shipment, o Order) []string {
	var out []string
	for _, s := range shipments {
		var names []string
		for _, k := range c.Carriers {
			k.Ship(s, o)
			names = append(names, k.Name())
		}
		out = append(out, fmt.Sprintf("%s: %s", s.Sender, strings.Join(names, ",")))
	}
	return out
}

// SortShipments sorts shipments in place by less.
func SortShipments(shipments []Shipment, less func(a, b Shipment) bool) {
	sort.SliceStable(shipments, func(i, j int) bool { return less(shipments[i], shipments[j]) })
}
