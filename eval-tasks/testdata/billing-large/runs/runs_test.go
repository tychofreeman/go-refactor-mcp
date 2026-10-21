package runs

import (
	"reflect"
	"testing"

	"example.com/billing/billing"
)

func TestRuns(t *testing.T) {
	w := &billing.WalletPayer{}
	c := billing.Config{Payers: []billing.Payer{w}}
	accounts := []billing.Account{{ID: "a1", Owner: "Ann", Wallet: "0xab"}}
	Rent(c, accounts)
	Training(c, accounts)
	if want := []string{"0xab: RENT-1", "0xab: TRAI-10"}; !reflect.DeepEqual(w.Charged, want) {
		t.Errorf("charged %q, want %q", w.Charged, want)
	}
}
