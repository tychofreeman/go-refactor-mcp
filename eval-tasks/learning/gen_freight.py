"""Generates eval-tasks/testdata/freight-xl, a module of the five shapes of
billing-large (near-duplicates, an additive field, two fields into a map,
a helper into a method, a parameter object) with many more uses of each,
its hidden tests (testdata/freight-*) and eval-tasks/derivations-freight.json.

Run from the repository root: python3 eval-tasks/learning/gen_freight.py
"""
import json
import os

ROOT = "eval-tasks/testdata"
MOD = os.path.join(ROOT, "freight-xl")


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text.lstrip("\n"))


# Carriers: type, name, field shown, how "to" is written, how "text" is.
CARRIERS = [
    ("Truck", "truck", "Road", "last(s.Road, 4)", "o.Ref"),
    ("Train", "train", "Rail", "s.Rail", 'o.Ref+fmt.Sprintf(" %dkg", o.Weight)'),
    ("Plane", "plane", "Air", "s.Air", "o.Ref"),
    ("Vessel", "vessel", "Sea", "s.Sea", "strings.ToLower(o.Ref)"),
    ("Van", "van", "Road", "s.Road", 'o.Ref+"/van"'),
    ("Freighter", "freighter", "Rail", "s.Rail", "strings.ToUpper(o.Ref)"),
    ("Drone", "drone", "Air", "s.Air", '"D-"+o.Ref'),
    ("Barge", "barge", "Sea", "s.Sea", "o.Ref"),
    ("Courier", "courier", "Sender", "s.Sender", "o.Ref"),
    ("Post", "post", "ID", "s.ID", 'fmt.Sprint(o.Weight)+"kg "+o.Ref'),
]


def carriers():
    out = []
    for typ, name, field, to, text in CARRIERS:
        out.append(f'''
// {typ} ships by {name}.
type {typ} struct {{
	Shipped []string // what was shipped, "{name}: ref"
}}

// Ship sends o to s by {name}.
func (c *{typ}) Ship(s Shipment, o Order) {{
	log.Printf("{name} ship to %s", {to})
	c.Shipped = append(c.Shipped, {to}+": "+{text})
}}

// Name is the carrier's name.
func (c *{typ}) Name() string {{ return "{name}" }}
''')
    return "".join(out)


CORE = '''
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
''' + "%CARRIERS%" + '''
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
'''

CORE_TEST = '''
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
'''

# Reader packages: functions over the Road and Rail fields, so that the
# fields are read many times in several packages.
READERS = '''
// Missing{F} lists senders without a {f} code.
func Missing{F}(ss []freight.Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.{F} == "" {
			out = append(out, s.Sender)
		}
	}
	return out
}

// {F}Region is the region that starts a shipment's {f} code.
func {F}Region(s freight.Shipment) string {
	if len(s.{F}) < 2 {
		return ""
	}
	return strings.ToUpper(s.{F}[:2])
}

// Same{F} reports whether a and b use the same {f} code.
func Same{F}(a, b freight.Shipment) bool {
	return strings.TrimSpace(a.{F}) == strings.TrimSpace(b.{F})
}

// By{F} indexes shipments by {f} code.
func By{F}(ss []freight.Shipment) map[string]freight.Shipment {
	m := map[string]freight.Shipment{}
	for _, s := range ss {
		m[s.{F}] = s
	}
	return m
}

// {F}Counts counts shipments by {f} code.
func {F}Counts(ss []freight.Shipment) map[string]int {
	m := map[string]int{}
	for _, s := range ss {
		if s.{F} != "" {
			m[s.{F}]++
		}
	}
	return m
}

// Upper{F} is a shipment's {f} code in capitals.
func Upper{F}(s freight.Shipment) string { return strings.ToUpper(s.{F}) }

// {F}Length is the length of a shipment's {f} code.
func {F}Length(s freight.Shipment) int { return len(s.{F}) }

// {F}Line is a shipment's {f} line for a manifest.
func {F}Line(s freight.Shipment) string {
	if s.{F} == "" {
		return s.Sender + ": no {f}"
	}
	return s.Sender + ": " + s.{F}
}
'''

MANIFESTS = '''
// Package manifests lists shipments' pickup points.
package manifests

import (
	"fmt"
	"sort"
	"strings"

	"example.com/freight/freight"
)

// Register is a line per shipment: sender, road and rail.
func Register(ss []freight.Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, fmt.Sprintf("%s [%s] %s", s.Sender, s.Road, s.Rail))
	}
	return out
}

// CSV lists shipments as "id,sender,road,rail" lines, by ID.
func CSV(ss []freight.Shipment) string {
	sorted := append([]freight.Shipment(nil), ss...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b strings.Builder
	for _, s := range sorted {
		fmt.Fprintf(&b, "%s,%s,%s,%s\\n", s.ID, s.Sender, s.Road, s.Rail)
	}
	return b.String()
}

// Routable reports whether s can go by road or rail.
func Routable(s freight.Shipment) bool { return s.Road != "" || s.Rail != "" }

// Label is how a shipment is shown: sender and the first route there is.
func Label(s freight.Shipment) string {
	switch {
	case s.Road != "":
		return s.Sender + " (" + s.Road + ")"
	case s.Rail != "":
		return s.Sender + " (" + s.Rail + ")"
	}
	return s.Sender
}
'''

AUDIT = '''
// Package audit checks shipments' pickup points.
package audit

import (
	"strings"

	"example.com/freight/freight"
)

// Both reports whether s has both a road and a rail code.
func Both(s freight.Shipment) bool { return s.Road != "" && s.Rail != "" }

// Either is the road code, or else the rail code.
func Either(s freight.Shipment) string {
	if s.Road != "" {
		return s.Road
	}
	return s.Rail
}

// Pairs is "road/rail" for each shipment.
func Pairs(ss []freight.Shipment) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Road+"/"+s.Rail)
	}
	return out
}
'''

READER_TESTS = {
    "manifests": '''
package manifests

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

var ss = []freight.Shipment{
	{ID: "2", Sender: "Bo", Road: "nw-0042", Rail: "Y7"},
	{ID: "1", Sender: "Ann", Road: "se-1111"},
	{ID: "3", Sender: "Cy", Rail: "Y9"},
}

func TestManifests(t *testing.T) {
	if got, want := Register(ss[:1]), []string{"Bo [nw-0042] Y7"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Register = %q, want %q", got, want)
	}
	if got, want := MissingRoad(ss), []string{"Cy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingRoad = %q", got)
	}
	if got, want := MissingRail(ss), []string{"Ann"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingRail = %q", got)
	}
	if got := RoadRegion(ss[0]); got != "NW" {
		t.Errorf("RoadRegion = %q", got)
	}
	if got := CSV(ss); got != "1,Ann,se-1111,\\n2,Bo,nw-0042,Y7\\n3,Cy,,Y9\\n" {
		t.Errorf("CSV = %q", got)
	}
	if got := Label(ss[2]); got != "Cy (Y9)" {
		t.Errorf("Label = %q", got)
	}
	if got := RailCounts(ss); got["Y7"] != 1 || got["Y9"] != 1 || len(got) != 2 {
		t.Errorf("RailCounts = %v", got)
	}
	if got := RoadLine(ss[2]); got != "Cy: no road" {
		t.Errorf("RoadLine = %q", got)
	}
}
''',
    "audit": '''
package audit

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

func TestAudit(t *testing.T) {
	ss := []freight.Shipment{{Sender: "Bo", Road: "nw-0042", Rail: "Y7"}, {Sender: "Cy", Rail: "Y9"}}
	if !Both(ss[0]) || Both(ss[1]) {
		t.Errorf("Both")
	}
	if got := Either(ss[1]); got != "Y9" {
		t.Errorf("Either = %q", got)
	}
	if got, want := Pairs(ss), []string{"nw-0042/Y7", "/Y9"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Pairs = %q", got)
	}
	if got := UpperRoad(ss[0]); got != "NW-0042" {
		t.Errorf("UpperRoad = %q", got)
	}
	if !SameRail(ss[0], freight.Shipment{Rail: " Y7 "}) {
		t.Errorf("SameRail")
	}
}
''',
}

# Callers of Dispatch, so that its signature has many calls.
SCHEDULES = [
    ("Daily", "DAY"), ("Weekly", "WEEK"), ("Monthly", "MONTH"), ("Express", "EXP"),
    ("Overnight", "NIGHT"), ("Bulk", "BULK"), ("Fragile", "FRAG"), ("Cold", "COLD"),
    ("Hazard", "HAZ"), ("Returns", "RET"), ("Samples", "SAMP"), ("Parts", "PART"),
    ("Retail", "RETL"), ("Wholesale", "WHOL"), ("Archive", "ARCH"),
]
DEPOTS = [
    ("North", "N"), ("South", "S"), ("East", "E"), ("West", "W"), ("Central", "C"),
    ("Harbor", "H"), ("Airport", "A"), ("Border", "B"), ("Valley", "V"), ("Coast", "K"),
    ("Hill", "L"), ("Lake", "Q"), ("River", "R"), ("Plain", "P"), ("Forest", "F"),
]


def schedules():
    out = ['''
// Package schedules makes the regular shipping runs.
package schedules

import "example.com/freight/freight"
''']
    for i, (name, ref) in enumerate(SCHEDULES, 1):
        out.append(f'''
// {name} ships the {name.lower()} order to shipments.
func {name}(c freight.Config, shipments []freight.Shipment) []string {{
	return freight.Dispatch(c, shipments, freight.Order{{Ref: "{ref}-{i}", Weight: {i * 10}}})
}}
''')
    return "".join(out)


def depots():
    out = ['''
// Package depots ships each depot's orders.
package depots

import "example.com/freight/freight"
''']
    for i, (name, code) in enumerate(DEPOTS, 1):
        out.append(f'''
// {name} ships o from the {name.lower()} depot to shipments that have a road code.
func {name}(c freight.Config, shipments []freight.Shipment, o freight.Order) []string {{
	var road []freight.Shipment
	for _, s := range shipments {{
		if s.Road != "" {{
			road = append(road, s)
		}}
	}}
	o.Ref = "{code}" + o.Ref
	return freight.Dispatch(c, road, o)
}}
''')
    return "".join(out)


SCHEDULES_TEST = '''
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
'''

DEPOTS_TEST = '''
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
'''

HIDDEN = {
    "dispatch": '''
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
''',
    "blocked": '''
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
''',
    "routes": '''
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
''',
    "tail": '''
package freight

import "testing"

func TestAcceptTail(t *testing.T) {
	c := &Truck{}
	if got := c.tail("12"); got != "12" {
		t.Errorf("tail(12) = %q", got)
	}
	if got := c.tail("RD-NORTH-0042"); got != "0042" {
		t.Errorf("tail cut to %q, want the last 4 bytes", got)
	}
	if got := c.Label(Shipment{Road: "RD-0004"}, Order{Ref: "R"}); got != "truck 0004 R" {
		t.Errorf("Label = %q", got)
	}
	if got := c.Tails([]Shipment{{Road: "1"}, {Road: "RD-0042"}}); len(got) != 2 || got[1] != "0042" {
		t.Errorf("Tails = %q", got)
	}
	if got := c.Badge(Shipment{Sender: "acme"}); got != "ACME" {
		t.Errorf("Badge = %q", got)
	}
}
''',
    "run": '''
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
''',
}

PROMPTS = {
    "dispatch": "The Ship methods of the ten carriers in package freight (Truck, Train, Plane, Vessel, Van, Freighter, Drone, Barge, Courier and Post) do nearly the same thing. Make all ten call one package-level function dispatch(shipped *[]string, mode, to, text string), which logs \"<mode> ship to <to>\" with the standard logger and appends \"<to>: <text>\" to *shipped. What they ship and log must not change.",
    "blocked": "Shipments can block carriers. Add a field Blocked map[string]bool to freight.Shipment, keyed by a carrier's name (what Carrier.Name returns). Dispatch does not ship through a carrier the shipment blocked, and that shipment's summary line lists only the carriers it shipped through.",
    "routes": "Replace the fields Road and Rail of freight.Shipment with one field Routes map[string]string, keyed by route: \"road\" for the road depot code and \"rail\" for the rail yard code. Everything that used them, in every package, must work as before.",
    "tail": "last is used only by Truck. Replace it with a method func (c *Truck) tail(code string) string that keeps the last 4 bytes of code, the part a driver sees, and remove last. What Truck's methods return and ship must not change.",
    "run": "Dispatch should take its shipments and order as one value. Add a type freight.Run struct { Shipments []Shipment; Order Order } and change Dispatch to func Dispatch(c Config, r Run) []string, updating every caller in every package. What it ships and returns must not change.",
}


def main():
    write(os.path.join(MOD, "go.mod"), "module example.com/freight\n\ngo 1.22\n")
    write(os.path.join(MOD, "freight/freight.go"), CORE.replace("%CARRIERS%", carriers()))
    write(os.path.join(MOD, "freight/freight_test.go"), CORE_TEST)
    readers = "".join(READERS.replace("{F}", f).replace("{f}", f.lower()) for f in ("Road", "Rail"))
    write(os.path.join(MOD, "manifests/manifests.go"), MANIFESTS + readers)
    write(os.path.join(MOD, "audit/audit.go"), AUDIT + readers.replace("Missing", "Lacking").replace("Region", "Zone"))
    for pkg, text in READER_TESTS.items():
        write(os.path.join(MOD, pkg, pkg + "_test.go"), text)
    write(os.path.join(MOD, "schedules/schedules.go"), schedules())
    write(os.path.join(MOD, "schedules/schedules_test.go"), SCHEDULES_TEST)
    write(os.path.join(MOD, "depots/depots.go"), depots())
    write(os.path.join(MOD, "depots/depots_test.go"), DEPOTS_TEST)
    tasks = []
    for name, test in HIDDEN.items():
        write(os.path.join(ROOT, "freight-" + name, "freight", name + "_test.go"), test)
        tasks.append({
            "name": "freight-" + name,
            "dir": "testdata/freight-xl",
            "tests": "testdata/freight-" + name,
            "check": "go vet ./... && go test ./...",
            "prompt": PROMPTS[name],
        })
    with open("eval-tasks/derivations-freight.json", "w") as f:
        json.dump(tasks, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
