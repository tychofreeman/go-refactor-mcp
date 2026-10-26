package m

import "strings"

// Plant is something in a bed.
type Plant struct {
	Title   string
	Species Species
}

// Species is a kind of plant.
type Species interface {
	Label(it Plant) string
}

// Fern is a species.
type Fern struct{}

// Moss is a species.
type Moss struct{}

func (f Fern) Label(it Plant) string { return it.Title }

func (m Moss) Label(p Plant) string {
	if p.Title == "" {
		return "moss"
	}
	return strings.ToUpper(p.Title) + " (moss)"
}

// Label shows it.
func Label(it Plant) string { return it.Species.Label(it) }

// FernLabel shows it as a fern.
func FernLabel(f Fern, it Plant) string { return f.Label(it) }
