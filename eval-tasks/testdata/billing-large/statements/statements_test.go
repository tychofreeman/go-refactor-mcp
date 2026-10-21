package statements

import (
	"reflect"
	"testing"

	"example.com/billing/billing"
)

var accounts = []billing.Account{
	{ID: "2", Owner: "Bo", Card: "5500000000000004", Bank: "fr76 01"},
	{ID: "1", Owner: "Ann", Card: "4111111111111111"},
	{ID: "3", Owner: "Cy", Bank: "DE89"},
}

func TestStatements(t *testing.T) {
	if got, want := Register(accounts[:1]), []string{"Bo [5500000000000004] fr76 01"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Register = %q, want %q", got, want)
	}
	if got, want := MissingCard(accounts), []string{"Cy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingCard = %q", got)
	}
	if got, want := MissingBank(accounts), []string{"Ann"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingBank = %q", got)
	}
	if got := BankCountry(accounts[0]); got != "FR" {
		t.Errorf("BankCountry = %q", got)
	}
	if got := CSV(accounts); got != "1,Ann,4111111111111111,\n2,Bo,5500000000000004,fr76 01\n3,Cy,,DE89\n" {
		t.Errorf("CSV = %q", got)
	}
	if got := Label(accounts[2]); got != "Cy (DE89)" {
		t.Errorf("Label = %q", got)
	}
	if got := Issuers(accounts); got["visa"] != 1 || got["mastercard"] != 1 {
		t.Errorf("Issuers = %v", got)
	}
}
