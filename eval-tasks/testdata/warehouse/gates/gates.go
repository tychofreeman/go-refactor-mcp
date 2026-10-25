// Package gates admits trucks to the dock doors of the warehouse.
package gates

// Yard holds the dock doors and what each truck has paid.
type Yard struct {
	Doors   []string          // door names, in the order they are tried
	Taken   map[string]string // door -> truck at it
	Paid    map[string]bool
	Charged []string // trucks charged the dock fee, in order
	slots   []int    // arrival slots still to hand out
}

// NewYard returns a yard with the given doors and arrival slots.
func NewYard(doors []string, slots ...int) *Yard {
	return &Yard{Doors: doors, Taken: map[string]string{}, Paid: map[string]bool{}, slots: slots}
}

// both reports whether a and b are both true.
func both(a, b bool) bool {
	return a && b
}

// either reports whether a or b is true.
func either(a, b bool) bool {
	return a || b
}

// within reports whether lo <= v <= hi.
func within(v, lo, hi int) bool {
	return lo <= v && v <= hi
}

// reserve puts truck at the first free door, and reports whether there was one.
func (y *Yard) reserve(truck string) bool {
	for _, d := range y.Doors {
		if _, ok := y.Taken[d]; !ok {
			y.Taken[d] = truck
			return true
		}
	}
	return false
}

// charge charges truck the dock fee, and reports whether it has paid.
func (y *Yard) charge(truck string) bool {
	y.Charged = append(y.Charged, truck)
	return y.Paid[truck]
}

// nextSlot hands out the next arrival slot, or 0 when none is left.
func (y *Yard) nextSlot() int {
	if len(y.slots) == 0 {
		return 0
	}
	s := y.slots[0]
	y.slots = y.slots[1:]
	return s
}

// Admit reserves a door for truck and charges it, and reports whether both succeeded.
func (y *Yard) Admit(truck string) bool {
	return both(y.reserve(truck), y.charge(truck))
}

// Pass lets a truck through when it is known or has paid.
func (y *Yard) Pass(truck string, known bool) bool {
	return either(known, y.Paid[truck])
}

// Express admits a truck that has paid or that can be charged.
func (y *Yard) Express(truck string) bool {
	return either(y.Paid[truck], y.charge(truck))
}

// OnTime reports whether the truck's arrival slot is in the morning window.
func (y *Yard) OnTime() bool {
	return within(y.nextSlot(), 6, 11)
}

// Fits reports whether n pallets fit a door.
func Fits(n int) bool {
	return within(n, 1, 24)
}
