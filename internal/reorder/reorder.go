// Package reorder decides, for either language, whether code can be
// evaluated in another place or another number of times, from facts about
// what it does and what the code it would pass does. Each language finds
// the facts (analysis for Go, tsapi for TypeScript) and words the refusal.
package reorder

// Times says how often code would be evaluated after a move, for each
// time it is evaluated now.
type Times int

const (
	Once    Times = iota // as often as now
	Maybe                // once or not at all (it moves under a condition)
	Several              // once for each of several uses, each evaluating it again
	Shared               // once, with the one value standing for several evaluations
	Unused               // once more or once less, where nothing uses the value (an argument added or dropped)
)

// Throw is how evaluating code may throw or panic.
type Throw int

const (
	NoThrow    Throw = iota
	OnNull           // only by reading through what may be nil, null or undefined
	OtherThrow       // another way, or a way not known
)

// Facts are what evaluating some code may do.
type Facts struct {
	Writes bool // changes state, or a variable code elsewhere reads
	Reads  bool // reads state other code may change
	Throws Throw
	Makes  bool // makes a new value, which another evaluation would not share
	Leaves bool // returns, breaks or continues out of the code
	Stops  bool // may not finish or return (a loop, os.Exit, code not known)
}

// Kind names what in Facts keeps code from moving.
type Kind int

const (
	None Kind = iota
	Writing
	Reading
	Throwing
	Making
	Leaving
	Stopping
)

// Repeat returns what in f keeps code from being evaluated t times as
// often as now, or None. With between, the code that runs between the
// evaluations is judged on its own (Conflict), so a read alone does not.
// One value standing for several evaluations may throw: it throws at the
// first either way.
func Repeat(t Times, f Facts, between bool) Kind {
	if t == Once {
		return None
	}
	switch {
	case f.Writes:
		return Writing
	case f.Leaves:
		return Leaving
	}
	switch t {
	case Maybe, Unused:
		if f.Throws != NoThrow {
			return Throwing
		}
	case Several, Shared:
		if f.Reads && !between {
			return Reading
		}
		if f.Throws != NoThrow && t == Several {
			return Throwing
		}
		if f.Makes {
			return Making
		}
	}
	return None
}

// Conflict returns what in e and what in p keep code that does e from
// being evaluated on the other side of code that does p, or None, None:
// one writes what the other reads or both act, where an act is a write, a
// throw, leaving or not finishing. A throw only through nil, null or
// undefined may pass a write: it throws either way, and only what the
// write did first differs (the user's choice, 2026-10-21).
func Conflict(e, p Facts) (Kind, Kind) {
	if e.Writes {
		switch {
		case p.Writes:
			return Writing, Writing
		case p.Stops:
			return Writing, Stopping
		case p.Throws == OtherThrow:
			return Writing, Throwing
		case p.Leaves:
			return Writing, Leaving
		case p.Reads:
			return Writing, Reading
		}
	}
	if e.Throws != NoThrow {
		switch {
		case p.Stops:
			return Throwing, Stopping
		case p.Leaves:
			return Throwing, Leaving
		case p.Writes && e.Throws == OtherThrow:
			return Throwing, Writing
		case p.Throws != NoThrow && (e.Throws == OtherThrow || p.Throws == OtherThrow):
			return Throwing, Throwing
		}
	}
	if e.Leaves {
		switch {
		case p.Writes:
			return Leaving, Writing
		case p.Throws != NoThrow:
			return Leaving, Throwing
		case p.Leaves:
			return Leaving, Leaving
		case p.Stops:
			return Leaving, Stopping
		}
	}
	if e.Reads && p.Writes {
		return Reading, Writing
	}
	return None, None
}
