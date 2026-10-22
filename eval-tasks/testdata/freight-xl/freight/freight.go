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
