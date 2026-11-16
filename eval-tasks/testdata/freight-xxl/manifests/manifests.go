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
		fmt.Fprintf(&b, "%s,%s,%s,%s\n", s.ID, s.Sender, s.Road, s.Rail)
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

// MissingRoad lists senders without a road code.
func MissingRoad(ss []freight.Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Road == "" {
			out = append(out, s.Sender)
		}
	}
	return out
}

// RoadRegion is the region that starts a shipment's road code.
func RoadRegion(s freight.Shipment) string {
	if len(s.Road) < 2 {
		return ""
	}
	return strings.ToUpper(s.Road[:2])
}

// SameRoad reports whether a and b use the same road code.
func SameRoad(a, b freight.Shipment) bool {
	return strings.TrimSpace(a.Road) == strings.TrimSpace(b.Road)
}

// ByRoad indexes shipments by road code.
func ByRoad(ss []freight.Shipment) map[string]freight.Shipment {
	m := map[string]freight.Shipment{}
	for _, s := range ss {
		m[s.Road] = s
	}
	return m
}

// RoadCounts counts shipments by road code.
func RoadCounts(ss []freight.Shipment) map[string]int {
	m := map[string]int{}
	for _, s := range ss {
		if s.Road != "" {
			m[s.Road]++
		}
	}
	return m
}

// UpperRoad is a shipment's road code in capitals.
func UpperRoad(s freight.Shipment) string { return strings.ToUpper(s.Road) }

// RoadLength is the length of a shipment's road code.
func RoadLength(s freight.Shipment) int { return len(s.Road) }

// RoadLine is a shipment's road line for a manifest.
func RoadLine(s freight.Shipment) string {
	if s.Road == "" {
		return s.Sender + ": no road"
	}
	return s.Sender + ": " + s.Road
}

// MissingRail lists senders without a rail code.
func MissingRail(ss []freight.Shipment) []string {
	var out []string
	for _, s := range ss {
		if s.Rail == "" {
			out = append(out, s.Sender)
		}
	}
	return out
}

// RailRegion is the region that starts a shipment's rail code.
func RailRegion(s freight.Shipment) string {
	if len(s.Rail) < 2 {
		return ""
	}
	return strings.ToUpper(s.Rail[:2])
}

// SameRail reports whether a and b use the same rail code.
func SameRail(a, b freight.Shipment) bool {
	return strings.TrimSpace(a.Rail) == strings.TrimSpace(b.Rail)
}

// ByRail indexes shipments by rail code.
func ByRail(ss []freight.Shipment) map[string]freight.Shipment {
	m := map[string]freight.Shipment{}
	for _, s := range ss {
		m[s.Rail] = s
	}
	return m
}

// RailCounts counts shipments by rail code.
func RailCounts(ss []freight.Shipment) map[string]int {
	m := map[string]int{}
	for _, s := range ss {
		if s.Rail != "" {
			m[s.Rail]++
		}
	}
	return m
}

// UpperRail is a shipment's rail code in capitals.
func UpperRail(s freight.Shipment) string { return strings.ToUpper(s.Rail) }

// RailLength is the length of a shipment's rail code.
func RailLength(s freight.Shipment) int { return len(s.Rail) }

// RailLine is a shipment's rail line for a manifest.
func RailLine(s freight.Shipment) string {
	if s.Rail == "" {
		return s.Sender + ": no rail"
	}
	return s.Sender + ": " + s.Rail
}
