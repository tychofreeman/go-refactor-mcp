// Package lending lends a library's items to its members.
package lending

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Item is something the library lends.
type Item struct {
	ID     string
	Title  string
	Kind   string // "book", "dvd" or "atlas"
	Copies int
}

// Member is someone who borrows.
type Member struct {
	ID     string
	Name   string
	Email  string
	Phone  string
	Street string
	City   string
	Zip    string
}

// Loan is an item lent to a member on a day.
type Loan struct {
	Item   Item
	Member Member
	Day    int // the day it was lent
}

// Charge is what a member owes for a loan returned late.
type Charge struct {
	Member string
	Item   string
	Fee    int // in cents
}

// LoanDays is how many days an item may be kept.
func LoanDays(it Item) int {
	switch it.Kind {
	case "book":
		return 21
	case "dvd":
		return 7
	case "atlas":
		return 3
	}
	return 0
}

// DailyFee is what each late day of an item costs, in cents.
func DailyFee(it Item) int {
	switch it.Kind {
	case "book":
		return 10
	case "dvd":
		return 50
	case "atlas":
		return 100
	}
	return 0
}

// Label is how an item is shown on a receipt.
func Label(it Item) string {
	switch it.Kind {
	case "book":
		return it.Title
	case "dvd":
		return "DVD: " + it.Title
	case "atlas":
		return strings.ToUpper(it.Title) + " (reference)"
	}
	return it.Title
}

// Shelf is where an item is kept.
func Shelf(it Item) string {
	switch it.Kind {
	case "book":
		return "stacks"
	case "dvd":
		return "media"
	case "atlas":
		return "reference"
	}
	return "returns"
}

// Renewable reports whether a loan of the item may be renewed.
func Renewable(it Item) bool {
	switch it.Kind {
	case "book", "dvd":
		return true
	}
	return false
}

// Policy says how late fees are reckoned.
type Policy struct {
	GraceDays int // late days not charged
	MaxFee    int // the most one loan is charged, in cents
	Rounding  int // fees are rounded up to a multiple of this, in cents
}

var policy = Policy{GraceDays: 2, MaxFee: 500, Rounding: 5}

// Settle charges, in order, each loan that is late by today beyond its
// grace days.
func Settle(loans []Loan, today int) []Charge {
	var out []Charge
	for _, l := range loans {
		if c, ok := charge(l, today); ok {
			out = append(out, c)
		}
	}
	return out
}

// Warn reports whether a loan is late by today but still within its
// grace days.
func Warn(l Loan, today int) bool {
	late := today - l.Day - LoanDays(l.Item)
	return late > 0 && late <= policy.GraceDays
}

func charge(l Loan, today int) (Charge, bool) {
	late := overdue(l, today)
	if late <= 0 {
		return Charge{}, false
	}
	return Charge{Member: l.Member.ID, Item: l.Item.ID, Fee: fee(l.Item, late)}, true
}

func overdue(l Loan, today int) int {
	return today - l.Day - LoanDays(l.Item) - policy.GraceDays
}

func fee(it Item, late int) int {
	return capFee(roundFee(late * DailyFee(it)))
}

func roundFee(f int) int {
	if r := policy.Rounding; r > 0 && f%r != 0 {
		return f + r - f%r
	}
	return f
}

func capFee(f int) int {
	if f > policy.MaxFee {
		return policy.MaxFee
	}
	return f
}

// Postal is a member's address as a letter shows it.
func Postal(m Member) string {
	return m.Street + ", " + m.Zip + " " + m.City
}

// Catalog is the items the library has, by ID.
type Catalog struct {
	Items map[string]Item
}

// Titles lists the titles of c's items, sorted.
func Titles(c *Catalog) []string {
	var out []string
	for _, it := range c.Items {
		out = append(out, it.Title)
	}
	sort.Strings(out)
	return out
}

// Import adds the items of text to c, one a line: "id|title|kind|copies".
// Blank lines and lines that start with # are skipped. If any line is
// wrong it adds nothing, and the error says which line: first any line
// that is not four fields with a number of copies, then the first item
// without an id, of an unknown kind, without copies, or already there.
func Import(c *Catalog, text string) error {
	var items []Item
	var lines []int
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != 4 {
			return fmt.Errorf("line %d: want 4 fields, got %d", i+1, len(f))
		}
		n, err := strconv.Atoi(strings.TrimSpace(f[3]))
		if err != nil {
			return fmt.Errorf("line %d: copies: %v", i+1, err)
		}
		items = append(items, Item{ID: strings.TrimSpace(f[0]), Title: strings.TrimSpace(f[1]), Kind: strings.TrimSpace(f[2]), Copies: n})
		lines = append(lines, i+1)
	}
	seen := map[string]bool{}
	for k, it := range items {
		if it.ID == "" {
			return fmt.Errorf("line %d: no id", lines[k])
		}
		if LoanDays(it) == 0 {
			return fmt.Errorf("line %d: unknown kind %q", lines[k], it.Kind)
		}
		if it.Copies <= 0 {
			return fmt.Errorf("line %d: no copies", lines[k])
		}
		if _, there := c.Items[it.ID]; there || seen[it.ID] {
			return fmt.Errorf("line %d: %s is already there", lines[k], it.ID)
		}
		seen[it.ID] = true
	}
	if c.Items == nil {
		c.Items = map[string]Item{}
	}
	for _, it := range items {
		c.Items[it.ID] = it
	}
	return nil
}
