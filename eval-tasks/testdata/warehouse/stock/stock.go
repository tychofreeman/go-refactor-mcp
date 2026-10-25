// Package stock keeps the count of goods in each bin of the warehouse.
package stock

import (
	"fmt"
	"strconv"
	"strings"
)

// Ledger counts the goods in each bin and records every move.
type Ledger struct {
	Bins  map[string]int
	Moves []string
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{Bins: map[string]int{}}
}

// Transfer takes qty goods out of one bin and puts them in another.
func (l *Ledger) Transfer(from, to string, qty int) {
	l.Bins[from] -= qty
	l.Bins[to] += qty
	l.Moves = append(l.Moves, fmt.Sprintf("%s>%s:%d", from, to, qty))
}

// Receive moves qty goods from the dock into each of bins.
func (l *Ledger) Receive(bins []string, qty int) {
	for _, b := range bins {
		l.Transfer("dock", b, qty)
	}
}

// Ship moves qty goods from bin to the outbound bay.
func (l *Ledger) Ship(bin string, qty int) {
	l.Transfer(bin, "bay", qty)
}

// Return moves goods sent back from the bay into bin.
func (l *Ledger) Return(bin string, qty int) {
	l.Transfer("bay", bin, qty)
}

// Consolidate empties each of bins into target.
func (l *Ledger) Consolidate(bins []string, target string) {
	for _, b := range bins {
		if b != target && l.Bins[b] > 0 {
			l.Transfer(b, target, l.Bins[b])
		}
	}
}

// PickList reads a list of moves written as "from to qty" triples.
type PickList struct {
	words []string
}

// ParsePickList splits text into the words of a pick list.
func ParsePickList(text string) *PickList {
	return &PickList{words: strings.Fields(text)}
}

// More reports whether a whole move is left to read.
func (p *PickList) More() bool {
	return len(p.words) >= 3
}

// Bin reads the next bin name.
func (p *PickList) Bin() string {
	w := p.words[0]
	p.words = p.words[1:]
	return w
}

// Qty reads the next quantity.
func (p *PickList) Qty() int {
	n, _ := strconv.Atoi(p.words[0])
	p.words = p.words[1:]
	return n
}

// Apply makes each move of a pick list.
func (l *Ledger) Apply(p *PickList) {
	for p.More() {
		l.Transfer(p.Bin(), p.Bin(), p.Qty())
	}
}
