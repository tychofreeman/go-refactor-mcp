package stock

import (
	"reflect"
	"testing"
)

func TestReceiveAndShip(t *testing.T) {
	l := NewLedger()
	l.Receive([]string{"a1", "a2"}, 5)
	l.Ship("a1", 2)
	l.Return("a1", 1)
	want := map[string]int{"dock": -10, "a1": 4, "a2": 5, "bay": 1}
	if !reflect.DeepEqual(l.Bins, want) {
		t.Errorf("bins = %v, want %v", l.Bins, want)
	}
	if got := l.Moves[0]; got != "dock>a1:5" {
		t.Errorf("first move = %q", got)
	}
}

func TestConsolidate(t *testing.T) {
	l := NewLedger()
	l.Receive([]string{"a1", "a2", "a3"}, 2)
	l.Consolidate([]string{"a1", "a2", "a3"}, "a2")
	if l.Bins["a2"] != 6 || l.Bins["a1"] != 0 || l.Bins["a3"] != 0 {
		t.Errorf("bins = %v", l.Bins)
	}
}
