package billing

import (
	"reflect"
	"testing"
)

func TestAcceptBlocked(t *testing.T) {
	c, b := &CardPayer{}, &BankPayer{}
	accounts := []Account{
		{ID: "a1", Owner: "Ann", Card: "4111", Bank: "DE1", Blocked: map[string]bool{"bank": true}},
		{ID: "a2", Owner: "Bo", Card: "5500", Bank: "DE2"},
	}
	got := Collect(Config{Payers: []Payer{c, b}}, accounts, Invoice{Ref: "r", Amount: 1})
	if want := []string{"Ann: card", "Bo: card,bank"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Collect = %q, want %q", got, want)
	}
	if want := []string{"DE2: r 1"}; !reflect.DeepEqual(b.Charged, want) {
		t.Errorf("bank charged %q, want %q", b.Charged, want)
	}
}
