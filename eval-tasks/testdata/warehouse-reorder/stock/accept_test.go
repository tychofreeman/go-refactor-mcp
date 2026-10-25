package stock

import (
	"reflect"
	"testing"
)

func TestAcceptTransferOrder(t *testing.T) {
	l := NewLedger()
	l.Transfer("a1", "b1", 3)
	if l.Bins["a1"] != 3 || l.Bins["b1"] != -3 {
		t.Errorf("Transfer(to, from, qty) moved the wrong way: %v", l.Bins)
	}
}

func TestAcceptApply(t *testing.T) {
	l := NewLedger()
	l.Apply(ParsePickList("a1 b1 3 b1 c1 1"))
	want := map[string]int{"a1": -3, "b1": 2, "c1": 1}
	if !reflect.DeepEqual(l.Bins, want) {
		t.Errorf("bins = %v, want %v", l.Bins, want)
	}
	if moves := []string{"a1>b1:3", "b1>c1:1"}; !reflect.DeepEqual(l.Moves, moves) {
		t.Errorf("moves = %q, want %q", l.Moves, moves)
	}
}

func TestAcceptConsolidate(t *testing.T) {
	l := NewLedger()
	l.Receive([]string{"a1", "a2"}, 2)
	l.Consolidate([]string{"a1", "a2"}, "a2")
	if l.Bins["a2"] != 4 || l.Bins["a1"] != 0 || l.Moves[2] != "a1>a2:2" {
		t.Errorf("bins = %v, moves = %q", l.Bins, l.Moves)
	}
}
