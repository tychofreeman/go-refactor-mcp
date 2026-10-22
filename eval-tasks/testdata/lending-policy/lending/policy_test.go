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
