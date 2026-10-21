package billing

import (
	"reflect"
	"testing"
)

func TestCollect(t *testing.T) {
	c, b := &CardPayer{}, &BankPayer{}
	accounts := []Account{{ID: "a1", Owner: "Ann", Card: "4111222233334444", Bank: "DE89"}}
	got := Collect(Config{Payers: []Payer{c, b}}, accounts, Invoice{Ref: "INV-1", Amount: 250})
	if want := []string{"Ann: card,bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Collect = %q, want %q", got, want)
	}
	if want := []string{"4444: INV-1"}; !reflect.DeepEqual(c.Charged, want) {
		t.Errorf("card charged %q, want %q", c.Charged, want)
	}
	if want := []string{"DE89: INV-1 250"}; !reflect.DeepEqual(b.Charged, want) {
		t.Errorf("bank charged %q, want %q", b.Charged, want)
	}
}

func TestCardTexts(t *testing.T) {
	c := &CardPayer{}
	if got := c.Masked("4111222233334444"); got != "****4444" {
		t.Errorf("Masked = %q", got)
	}
	if got := c.Hint(42); got != "0042" {
		t.Errorf("Hint = %q", got)
	}
}
