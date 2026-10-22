// Package members lists the library's members.
package members

import (
	"fmt"
	"sort"
	"strings"

	"example.com/lending/lending"
)

// Envelope is the address block for a letter to m.
func Envelope(m lending.Member) string {
	return fmt.Sprintf("%s\n%s\n%s %s", m.Name, m.Street, m.Zip, m.City)
}

// Local lists the members who live in city, sorted by name.
func Local(ms []lending.Member, city string) []string {
	var out []string
	for _, m := range ms {
		if strings.EqualFold(m.City, city) {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Complete reports whether m's address has every part.
func Complete(m lending.Member) bool { return m.Street != "" && m.City != "" && m.Zip != "" }

// Area is the first two digits of m's zip code.
func Area(m lending.Member) string {
	if len(m.Zip) < 2 {
		return ""
	}
	return m.Zip[:2]
}

// MissingStreet lists members without a street.
func MissingStreet(ms []lending.Member) []string {
	var out []string
	for _, m := range ms {
		if m.Street == "" {
			out = append(out, m.Name)
		}
	}
	return out
}

// ByStreet groups members' names by street.
func ByStreet(ms []lending.Member) map[string][]string {
	g := map[string][]string{}
	for _, m := range ms {
		g[m.Street] = append(g[m.Street], m.Name)
	}
	return g
}

// StreetCounts counts members by street.
func StreetCounts(ms []lending.Member) map[string]int {
	n := map[string]int{}
	for _, m := range ms {
		if m.Street != "" {
			n[m.Street]++
		}
	}
	return n
}

// SameStreet reports whether two members share a street.
func SameStreet(a, b lending.Member) bool {
	return strings.EqualFold(strings.TrimSpace(a.Street), strings.TrimSpace(b.Street))
}

// UpperStreet is a member's street in capitals.
func UpperStreet(m lending.Member) string { return strings.ToUpper(m.Street) }

// StreetLine is a member's street line for a list.
func StreetLine(m lending.Member) string {
	if m.Street == "" {
		return m.Name + ": no street"
	}
	return m.Name + ": " + m.Street
}

// MissingCity lists members without a city.
func MissingCity(ms []lending.Member) []string {
	var out []string
	for _, m := range ms {
		if m.City == "" {
			out = append(out, m.Name)
		}
	}
	return out
}

// ByCity groups members' names by city.
func ByCity(ms []lending.Member) map[string][]string {
	g := map[string][]string{}
	for _, m := range ms {
		g[m.City] = append(g[m.City], m.Name)
	}
	return g
}

// CityCounts counts members by city.
func CityCounts(ms []lending.Member) map[string]int {
	n := map[string]int{}
	for _, m := range ms {
		if m.City != "" {
			n[m.City]++
		}
	}
	return n
}

// SameCity reports whether two members share a city.
func SameCity(a, b lending.Member) bool {
	return strings.EqualFold(strings.TrimSpace(a.City), strings.TrimSpace(b.City))
}

// UpperCity is a member's city in capitals.
func UpperCity(m lending.Member) string { return strings.ToUpper(m.City) }

// CityLine is a member's city line for a list.
func CityLine(m lending.Member) string {
	if m.City == "" {
		return m.Name + ": no city"
	}
	return m.Name + ": " + m.City
}

// MissingZip lists members without a zip.
func MissingZip(ms []lending.Member) []string {
	var out []string
	for _, m := range ms {
		if m.Zip == "" {
			out = append(out, m.Name)
		}
	}
	return out
}

// ByZip groups members' names by zip.
func ByZip(ms []lending.Member) map[string][]string {
	g := map[string][]string{}
	for _, m := range ms {
		g[m.Zip] = append(g[m.Zip], m.Name)
	}
	return g
}

// ZipCounts counts members by zip.
func ZipCounts(ms []lending.Member) map[string]int {
	n := map[string]int{}
	for _, m := range ms {
		if m.Zip != "" {
			n[m.Zip]++
		}
	}
	return n
}

// SameZip reports whether two members share a zip.
func SameZip(a, b lending.Member) bool {
	return strings.EqualFold(strings.TrimSpace(a.Zip), strings.TrimSpace(b.Zip))
}

// UpperZip is a member's zip in capitals.
func UpperZip(m lending.Member) string { return strings.ToUpper(m.Zip) }

// ZipLine is a member's zip line for a list.
func ZipLine(m lending.Member) string {
	if m.Zip == "" {
		return m.Name + ": no zip"
	}
	return m.Name + ": " + m.Zip
}
