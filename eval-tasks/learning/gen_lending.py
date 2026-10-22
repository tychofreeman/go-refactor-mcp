"""Generates eval-tasks/testdata/lending-xl, set C of the experiment in
docs/reading-and-deriving.md (step 3): a module with many uses of five
shapes that no move, lesson or tool description names: a function split
into phases, a switch on a kind repeated in several functions, a package
variable read deep in a call chain, a type split into a named field, and
an additive feature that needs the phases split first. Also its hidden
tests (testdata/lending-*) and eval-tasks/derivations-lending.json.

Run from the repository root: python3 eval-tasks/learning/gen_lending.py
"""
import json
import os

ROOT = "eval-tasks/testdata"
MOD = os.path.join(ROOT, "lending-xl")


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text.lstrip("\n"))


CORE = '''
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
	for i, line := range strings.Split(text, "\\n") {
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
'''

CORE_TEST = '''
package lending

import (
	"reflect"
	"testing"
)

func TestKinds(t *testing.T) {
	b, d, a := Item{Title: "Emma", Kind: "book"}, Item{Title: "Heat", Kind: "dvd"}, Item{Title: "World", Kind: "atlas"}
	if LoanDays(b) != 21 || LoanDays(d) != 7 || LoanDays(a) != 3 {
		t.Errorf("LoanDays")
	}
	if Label(d) != "DVD: Heat" || Label(a) != "WORLD (reference)" || Shelf(a) != "reference" || Renewable(a) {
		t.Errorf("Label, Shelf or Renewable")
	}
}

func TestSettle(t *testing.T) {
	l := Loan{Item: Item{ID: "i", Kind: "dvd"}, Member: Member{ID: "m"}, Day: 0}
	if got, want := Settle([]Loan{l}, 12), []Charge{{Member: "m", Item: "i", Fee: 150}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
	if got := Settle([]Loan{l}, 30); got[0].Fee != 500 {
		t.Errorf("Settle capped = %v", got)
	}
	if !Warn(l, 9) || Warn(l, 10) {
		t.Errorf("Warn")
	}
}

func TestImport(t *testing.T) {
	c := &Catalog{}
	if err := Import(c, "a|Emma|book|2\\n\\n# films\\nb|Heat|dvd|1"); err != nil {
		t.Fatal(err)
	}
	if got := Titles(c); !reflect.DeepEqual(got, []string{"Emma", "Heat"}) {
		t.Errorf("Titles = %q", got)
	}
	for text, want := range map[string]string{
		"x|X|book":       "line 1: want 4 fields, got 3",
		"x|X|vinyl|1":    `line 1: unknown kind "vinyl"`,
		"\\nx|X|book|0":   "line 2: no copies",
		"a|Again|book|1": "line 1: a is already there",
	} {
		if err := Import(c, text); err == nil || err.Error() != want {
			t.Errorf("Import(%q) = %v, want %s", text, err, want)
		}
	}
	if len(c.Items) != 2 {
		t.Errorf("a failed import added items: %v", c.Items)
	}
}

func TestPostal(t *testing.T) {
	if got := Postal(Member{Street: "1 Elm", City: "Oslo", Zip: "0150"}); got != "1 Elm, 0150 Oslo" {
		t.Errorf("Postal = %q", got)
	}
}
'''

# members and notices read the address fields many times.
READERS = '''
// Missing{F} lists members without a {f}.
func Missing{F}(ms []lending.Member) []string {
	var out []string
	for _, m := range ms {
		if m.{F} == "" {
			out = append(out, m.Name)
		}
	}
	return out
}

// By{F} groups members' names by {f}.
func By{F}(ms []lending.Member) map[string][]string {
	g := map[string][]string{}
	for _, m := range ms {
		g[m.{F}] = append(g[m.{F}], m.Name)
	}
	return g
}

// {F}Counts counts members by {f}.
func {F}Counts(ms []lending.Member) map[string]int {
	n := map[string]int{}
	for _, m := range ms {
		if m.{F} != "" {
			n[m.{F}]++
		}
	}
	return n
}

// Same{F} reports whether two members share a {f}.
func Same{F}(a, b lending.Member) bool {
	return strings.EqualFold(strings.TrimSpace(a.{F}), strings.TrimSpace(b.{F}))
}

// Upper{F} is a member's {f} in capitals.
func Upper{F}(m lending.Member) string { return strings.ToUpper(m.{F}) }

// {F}Line is a member's {f} line for a list.
func {F}Line(m lending.Member) string {
	if m.{F} == "" {
		return m.Name + ": no {f}"
	}
	return m.Name + ": " + m.{F}
}
'''

MEMBERS = '''
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
	return fmt.Sprintf("%s\\n%s\\n%s %s", m.Name, m.Street, m.Zip, m.City)
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
'''

NOTICES = '''
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
'''

DESKS = [
    ("Main", 1), ("North", 2), ("South", 3), ("East", 4), ("West", 5),
    ("Harbor", 6), ("Campus", 7), ("Hospital", 8), ("Prison", 9), ("School", 10),
    ("Market", 11), ("Station", 12), ("Airport", 13), ("Museum", 14), ("Mobile", 15),
]


def desk():
    out = ['''
// Package desk settles the loans returned at each of the library's desks.
package desk

import "example.com/lending/lending"
''']
    for name, n in DESKS:
        out.append(f'''
// {name} settles the loans returned at the {name.lower()} desk on day.
func {name}(loans []lending.Loan, day int) []lending.Charge {{
	return lending.Settle(loans, day+{n % 3})
}}
''')
    out.append('''
// Rare settles the loans of atlases returned at any desk on day.
func Rare(loans []lending.Loan, day int) []lending.Charge {
	var rare []lending.Loan
	for _, l := range loans {
		if l.Item.Kind == "atlas" {
			rare = append(rare, l)
		}
	}
	return lending.Settle(rare, day)
}

// Owed is the total each member owes for loans returned on day.
func Owed(loans []lending.Loan, day int) map[string]int {
	owed := map[string]int{}
	for _, c := range lending.Settle(loans, day) {
		owed[c.Member] += c.Fee
	}
	return owed
}
''')
    return "".join(out)


FEEDS = [
    ("Novels", "n1|Emma|book|2\\nn2|Dracula|book|1"),
    ("Films", "f1|Heat|dvd|3\\nf2|Alien|dvd|1"),
    ("Maps", "m1|World|atlas|1"),
    ("Poetry", "p1|Odes|book|1\\np2|Psalms|book|2"),
    ("History", "h1|Rome|book|2\\n# atlases\\nh2|Empires|atlas|1"),
    ("Science", "s1|Cosmos|dvd|1\\ns2|Origins|book|1"),
    ("Children", "c1|Matilda|book|4"),
    ("Music", "u1|Jazz|dvd|2"),
    ("Travel", "t1|Alps|atlas|1\\nt2|Coasts|book|1"),
    ("Archive", "a1|Ledger|book|1"),
]


def feeds():
    out = ['''
// Package feeds loads the catalog from the library's suppliers.
package feeds

import (
	"strings"

	"example.com/lending/lending"
)
''']
    for name, text in FEEDS:
        out.append(f'''
// {name} loads the {name.lower()} feed into c.
func {name}(c *lending.Catalog) error {{
	return lending.Import(c, "{text}")
}}
''')
    out.append('''
// Preview lists the titles of text's well-formed lines, without adding
// anything.
func Preview(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) == 4 {
			out = append(out, strings.TrimSpace(f[1]))
		}
	}
	return out
}
''')
    return "".join(out)


VISIBLE = {
    "members/members_test.go": '''
package members

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

var ms = []lending.Member{
	{Name: "Bo", Street: "2 Oak", City: "Oslo", Zip: "0150"},
	{Name: "Ann", City: "oslo"},
	{Name: "Cy", Street: "9 Elm", City: "Bergen", Zip: "5003"},
}

func TestMembers(t *testing.T) {
	if got := Envelope(ms[0]); got != "Bo\\n2 Oak\\n0150 Oslo" {
		t.Errorf("Envelope = %q", got)
	}
	if got, want := Local(ms, "OSLO"), []string{"Ann", "Bo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Local = %q", got)
	}
	if got, want := MissingStreet(ms), []string{"Ann"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingStreet = %q", got)
	}
	if got := CityCounts(ms); got["Oslo"] != 1 || got["oslo"] != 1 || got["Bergen"] != 1 {
		t.Errorf("CityCounts = %v", got)
	}
	if !SameCity(ms[0], ms[1]) || Complete(ms[1]) || Area(ms[2]) != "50" || ZipLine(ms[1]) != "Ann: no zip" {
		t.Errorf("SameCity, Complete, Area or ZipLine")
	}
}
''',
    "notices/notices_test.go": '''
package notices

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

func TestNotices(t *testing.T) {
	l := lending.Loan{Item: lending.Item{ID: "i", Title: "Heat", Kind: "dvd"}, Member: lending.Member{Name: "Ann", Phone: "555", City: " Oslo "}, Day: 0}
	if got, want := Reminders([]lending.Loan{l}, 8), []string{"Ann: DVD: Heat is due"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Reminders = %q", got)
	}
	if got := Courtesy([]lending.Loan{l}, 12); got != nil {
		t.Errorf("Courtesy after the grace days = %q", got)
	}
	if !Media(l.Item) || Fragile(l.Item) || Header(l.Member) != "To Ann, Oslo" || UpperStreet(l.Member) != "" {
		t.Errorf("Media, Fragile, Header or UpperStreet")
	}
}
''',
    "desk/desk_test.go": '''
package desk

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

func TestDesk(t *testing.T) {
	ls := []lending.Loan{
		{Item: lending.Item{ID: "i", Kind: "atlas"}, Member: lending.Member{ID: "m"}, Day: 0},
		{Item: lending.Item{ID: "j", Kind: "book"}, Member: lending.Member{ID: "m"}, Day: 0},
	}
	if got, want := Rare(ls, 7), []lending.Charge{{Member: "m", Item: "i", Fee: 200}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Rare = %v, want %v", got, want)
	}
	if got := Owed(ls, 30); got["m"] != 570 {
		t.Errorf("Owed = %v", got)
	}
	if got := North(ls, 5); len(got) != 1 || got[0].Fee != 200 {
		t.Errorf("North = %v", got)
	}
}
''',
    "feeds/feeds_test.go": '''
package feeds

import (
	"reflect"
	"testing"

	"example.com/lending/lending"
)

func TestFeeds(t *testing.T) {
	c := &lending.Catalog{}
	for _, f := range []func(*lending.Catalog) error{Novels, Films, Maps, History} {
		if err := f(c); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Items) != 7 {
		t.Errorf("loaded %d items", len(c.Items))
	}
	if err := Novels(c); err == nil {
		t.Errorf("loading novels twice did not fail")
	}
	if got, want := Preview("a|Emma|book|1\\nbad\\n# x\\nb|Heat|dvd|2"), []string{"Emma", "Heat"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Preview = %q", got)
	}
}
''',
}

HIDDEN = {
    "phases": '''
package lending

import (
	"reflect"
	"testing"
)

func TestAcceptPhases(t *testing.T) {
	recs, err := ParseRecords("a|Emma|book|2\\n\\n# films\\nb|Heat|dvd|1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{{Line: 1, ID: "a", Title: "Emma", Kind: "book", Copies: 2}, {Line: 4, ID: "b", Title: "Heat", Kind: "dvd", Copies: 1}}
	if !reflect.DeepEqual(recs, want) {
		t.Errorf("ParseRecords = %v, want %v", recs, want)
	}
	if _, err := ParseRecords("a|Emma|book"); err == nil || err.Error() != "line 1: want 4 fields, got 3" {
		t.Errorf("ParseRecords of 3 fields: %v", err)
	}
	if _, err := ParseRecords("a|Emma|book|x"); err == nil || err.Error()[:14] != "line 1: copie" + "s" {
		t.Errorf("ParseRecords of bad copies: %v", err)
	}
	c := &Catalog{Items: map[string]Item{"a": {ID: "a", Title: "Old", Kind: "book", Copies: 1}}}
	if err := ValidateRecords(c, recs); err == nil || err.Error() != "line 1: a is already there" {
		t.Errorf("ValidateRecords of a duplicate: %v", err)
	}
	for _, r := range []struct {
		rec  Record
		want string
	}{
		{Record{Line: 3, ID: "z", Kind: "vinyl", Copies: 1}, `line 3: unknown kind "vinyl"`},
		{Record{Line: 5, Kind: "book", Copies: 1}, "line 5: no id"},
		{Record{Line: 6, ID: "y", Kind: "book"}, "line 6: no copies"},
	} {
		if err := ValidateRecords(&Catalog{}, []Record{r.rec}); err == nil || err.Error() != r.want {
			t.Errorf("ValidateRecords(%v) = %v, want %s", r.rec, err, r.want)
		}
	}
	empty := &Catalog{}
	if err := ValidateRecords(empty, recs); err != nil {
		t.Fatal(err)
	}
	ApplyRecords(empty, recs)
	if got := empty.Items["b"]; got != (Item{ID: "b", Title: "Heat", Kind: "dvd", Copies: 1}) {
		t.Errorf("ApplyRecords added %v", got)
	}
	if err := Import(empty, "c|Odes|book|1\\nc|Odes|book|1"); err == nil || err.Error() != "line 2: c is already there" {
		t.Errorf("Import of a repeated id: %v", err)
	}
	if len(empty.Items) != 2 {
		t.Errorf("a failed Import added items")
	}
}
''',
    "kinds": '''
package lending

import (
	"reflect"
	"testing"
)

func TestAcceptKinds(t *testing.T) {
	var _ Kind = Book{}
	var _ Kind = DVD{}
	var _ Kind = Atlas{}
	b, d, a := Item{Title: "Emma", Kind: Book{}}, Item{Title: "Heat", Kind: DVD{}}, Item{Title: "World", Kind: Atlas{}}
	if LoanDays(b) != 21 || LoanDays(d) != 7 || LoanDays(a) != 3 {
		t.Errorf("LoanDays")
	}
	if DailyFee(b) != 10 || DailyFee(d) != 50 || DailyFee(a) != 100 {
		t.Errorf("DailyFee")
	}
	if Label(b) != "Emma" || Label(d) != "DVD: Heat" || Label(a) != "WORLD (reference)" {
		t.Errorf("Label")
	}
	if Shelf(b) != "stacks" || Shelf(d) != "media" || Shelf(a) != "reference" {
		t.Errorf("Shelf")
	}
	if !Renewable(b) || !Renewable(d) || Renewable(a) {
		t.Errorf("Renewable")
	}
	c := &Catalog{}
	if err := Import(c, "x|Emma|book|1\\ny|Heat|dvd|1\\nz|World|atlas|1"); err != nil {
		t.Fatal(err)
	}
	if c.Items["x"].Kind != (Book{}) || c.Items["y"].Kind != (DVD{}) || c.Items["z"].Kind != (Atlas{}) {
		t.Errorf("Import made kinds %v", c.Items)
	}
	if err := Import(c, "q|Q|vinyl|1"); err == nil || err.Error() != `line 1: unknown kind "vinyl"` {
		t.Errorf("Import of an unknown kind: %v", err)
	}
	got := Settle([]Loan{{Item: Item{ID: "i", Kind: DVD{}}, Member: Member{ID: "m"}, Day: 0}}, 12)
	if want := []Charge{{Member: "m", Item: "i", Fee: 150}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle = %v, want %v", got, want)
	}
}
''',
    "policy": '''
package lending

import (
	"reflect"
	"testing"
)

func TestAcceptPolicy(t *testing.T) {
	if got := DefaultPolicy(); got != (Policy{GraceDays: 2, MaxFee: 500, Rounding: 5}) {
		t.Errorf("DefaultPolicy = %v", got)
	}
	l := Loan{Item: Item{ID: "i", Kind: "book"}, Member: Member{ID: "m"}, Day: 0}
	strict := Policy{GraceDays: 0, MaxFee: 1000, Rounding: 0}
	if got, want := Settle(strict, []Loan{l}, 30), []Charge{{Member: "m", Item: "i", Fee: 90}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle(strict) = %v, want %v", got, want)
	}
	if got, want := Settle(DefaultPolicy(), []Loan{l}, 30), []Charge{{Member: "m", Item: "i", Fee: 70}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Settle(default) = %v, want %v", got, want)
	}
	if got := Settle(Policy{GraceDays: 2, MaxFee: 40, Rounding: 25}, []Loan{l}, 25); got[0].Fee != 25 {
		t.Errorf("Settle rounded and capped = %v", got)
	}
	if !Warn(Policy{GraceDays: 5}, l, 25) || Warn(DefaultPolicy(), l, 25) {
		t.Errorf("Warn")
	}
}
''',
    "address": '''
package lending

import "testing"

func TestAcceptAddress(t *testing.T) {
	m := Member{ID: "m", Name: "Ann", Home: Address{Street: "1 Elm", City: "Oslo", Zip: "0150"}}
	if m.Home.City != "Oslo" {
		t.Errorf("Home.City = %q", m.Home.City)
	}
	if got := Postal(m); got != "1 Elm, 0150 Oslo" {
		t.Errorf("Postal = %q", got)
	}
}
''',
    "dryrun": '''
package lending

import (
	"reflect"
	"testing"
)

func TestAcceptDryRun(t *testing.T) {
	c := &Catalog{Items: map[string]Item{"a": {ID: "a", Title: "Old", Kind: "book", Copies: 1}}}
	titles, err := DryRun(c, "b|Emma|book|2\\n# films\\nd|Heat|dvd|1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Emma", "Heat"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("DryRun = %q, want %q", titles, want)
	}
	if len(c.Items) != 1 {
		t.Errorf("DryRun added items: %v", c.Items)
	}
	for text, want := range map[string]string{
		"a|Again|book|1":   "line 1: a is already there",
		"b|Emma|book":      "line 1: want 4 fields, got 3",
		"\\nb|Emma|tape|1": `line 2: unknown kind "tape"`,
	} {
		if _, err := DryRun(c, text); err == nil || err.Error() != want {
			t.Errorf("DryRun(%q) = %v, want %s", text, err, want)
		}
	}
}
''',
}

PROMPTS = {
    "phases": "Import in package lending parses, checks and adds in one function. Split it into three, in package lending: type Record struct { Line int; ID, Title, Kind string; Copies int } (Line is the 1-based line of text it came from); func ParseRecords(text string) ([]Record, error), which reads the lines as Import's first loop does and fails as it does on a line without 4 fields or with copies that are not a number; func ValidateRecords(c *Catalog, recs []Record) error, which checks them as Import's second loop does, failing, in this order for each record, on no id, an unknown kind, no copies, or an id already in c or earlier in recs, with Import's messages (\"line N: no id\", \"line N: unknown kind \\\"K\\\"\", \"line N: no copies\", \"line N: ID is already there\"); and func ApplyRecords(c *Catalog, recs []Record), which adds them to c. Import calls the three in turn. What Import does and returns must not change.",
    "kinds": "Each kind of item in package lending should be a type, not a string. Add an interface Kind { LoanDays() int; DailyFee() int; Label(title string) string; Shelf() string; Renewable() bool } and three types that implement it, Book, DVD and Atlas (each an empty struct), with what the switches in LoanDays, DailyFee, Label, Shelf and Renewable do for \"book\", \"dvd\" and \"atlas\". Item.Kind becomes a Kind (Item{Kind: DVD{}}). LoanDays(it), DailyFee(it), Label(it), Shelf(it) and Renewable(it) keep their signatures and results; Import turns \"book\", \"dvd\" and \"atlas\" into Book{}, DVD{} and Atlas{} and still fails with line N: unknown kind \"K\" on any other. Everything that compared a Kind with a string, in every package, must work as before.",
    "policy": "Remove the package variable policy of package lending. Add func DefaultPolicy() Policy, which returns Policy{GraceDays: 2, MaxFee: 500, Rounding: 5}. Settle and Warn take a Policy as their first parameter, func Settle(p Policy, loans []Loan, today int) []Charge and func Warn(p Policy, l Loan, today int) bool, and pass it to the functions that used the variable. Every caller in every package passes DefaultPolicy(). What they charge and report must not change.",
    "address": "Move the fields Street, City and Zip of lending.Member into a new type lending.Address struct { Street, City, Zip string }, and give Member a field Home Address in their place, so that a member is written Member{Home: Address{City: \"Oslo\"}} and its city read m.Home.City. Everything that used them, in every package, must work as before.",
    "dryrun": "Add func DryRun(c *Catalog, text string) ([]string, error) to package lending. It checks text exactly as Import does, failing with the error Import would return, but adds nothing to c; on success it returns the titles of the items Import would add, in the order of the text.",
}


def main():
    write(os.path.join(MOD, "go.mod"), "module example.com/lending\n\ngo 1.22\n")
    write(os.path.join(MOD, "lending/lending.go"), CORE)
    write(os.path.join(MOD, "lending/lending_test.go"), CORE_TEST)
    readers = "".join(READERS.replace("{F}", f).replace("{f}", f.lower()) for f in ("Street", "City", "Zip"))
    write(os.path.join(MOD, "members/members.go"), MEMBERS + readers)
    write(os.path.join(MOD, "notices/notices.go"), NOTICES + readers.replace("Missing", "Lacking").replace("Counts", "Tally"))
    write(os.path.join(MOD, "desk/desk.go"), desk())
    write(os.path.join(MOD, "feeds/feeds.go"), feeds())
    for path, text in VISIBLE.items():
        write(os.path.join(MOD, path), text)
    tasks = []
    for name, test in HIDDEN.items():
        write(os.path.join(ROOT, "lending-" + name, "lending", name + "_test.go"), test)
        tasks.append({
            "name": "lending-" + name,
            "dir": "testdata/lending-xl",
            "tests": "testdata/lending-" + name,
            "check": "go vet ./... && go test ./...",
            "prompt": PROMPTS[name],
        })
    with open("eval-tasks/derivations-lending.json", "w") as f:
        json.dump(tasks, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
