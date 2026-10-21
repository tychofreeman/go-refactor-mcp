package billing

import (
	"reflect"
	"testing"
)

func TestAcceptMethods(t *testing.T) {
	c, b := &CardPayer{}, &BankPayer{}
	a := Account{ID: "a1", Owner: "Ann", Methods: map[string]string{"card": "4111222233334444", "bank": "DE89"}}
	got := Collect(Config{Payers: []Payer{c, b}}, []Account{a}, Invoice{Ref: "INV-1", Amount: 250})
	if want := []string{"Ann: card,bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Collect = %q, want %q", got, want)
	}
	if want := []string{"4444: INV-1"}; !reflect.DeepEqual(c.Charged, want) {
		t.Errorf("card charged %q, want %q", c.Charged, want)
	}
	if want := []string{"DE89: INV-1 250"}; !reflect.DeepEqual(b.Charged, want) {
		t.Errorf("bank charged %q, want %q", b.Charged, want)
	}
	if got := c.Receipt(a, Invoice{Ref: "R"}); got != "card 4444 R" {
		t.Errorf("Receipt = %q", got)
	}
}
