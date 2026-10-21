package billing

import (
	"reflect"
	"testing"
)

func TestAcceptRun(t *testing.T) {
	w := &WalletPayer{}
	got := Collect(Config{Payers: []Payer{w}}, Run{Accounts: []Account{{ID: "a1", Owner: "Ann", Wallet: "0xab"}}, Invoice: Invoice{Ref: "r"}})
	if want := []string{"Ann: wallet"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Collect = %q, want %q", got, want)
	}
	if want := []string{"0xab: r"}; !reflect.DeepEqual(w.Charged, want) {
		t.Errorf("wallet charged %q, want %q", w.Charged, want)
	}
}
