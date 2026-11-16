// Package tracking checks shipments' pickup points.
package tracking

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
