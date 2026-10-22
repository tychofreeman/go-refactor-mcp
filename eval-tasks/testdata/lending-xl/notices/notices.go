// Package notices writes the library's letters and reminders.
package notices

import (
	"strings"

	"example.com/lending/lending"
)

// Reminder is the reminder for a loan within its grace days, or "".
func Reminder(l lending.Loan, today int) string {
	if !lending.Warn(l, today) {
		return ""
	}
	return l.Member.Name + ": " + lending.Label(l.Item) + " is due"
}

// Reminders lists the reminders due today.
func Reminders(loans []lending.Loan, today int) []string {
	var out []string
	for _, l := range loans {
		if r := Reminder(l, today); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// Courtesy lists the members to call today about a loan in its grace days.
func Courtesy(loans []lending.Loan, today int) []string {
	var out []string
	for _, l := range loans {
		if lending.Warn(l, today) && l.Member.Phone != "" {
			out = append(out, l.Member.Name+" "+l.Member.Phone)
		}
	}
	return out
}

// Fragile reports whether an item must be packed with care.
func Fragile(it lending.Item) bool { return it.Kind == "atlas" }

// Media reports whether an item goes back to the media desk.
func Media(it lending.Item) bool { return it.Kind == "dvd" }

// Header is the first line of a letter to m.
func Header(m lending.Member) string {
	return "To " + m.Name + ", " + strings.TrimSpace(m.City)
}

// LackingStreet lists members without a street.
func LackingStreet(ms []lending.Member) []string {
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

// StreetTally counts members by street.
func StreetTally(ms []lending.Member) map[string]int {
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

// LackingCity lists members without a city.
func LackingCity(ms []lending.Member) []string {
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

// CityTally counts members by city.
func CityTally(ms []lending.Member) map[string]int {
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

// LackingZip lists members without a zip.
func LackingZip(ms []lending.Member) []string {
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

// ZipTally counts members by zip.
func ZipTally(ms []lending.Member) map[string]int {
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
