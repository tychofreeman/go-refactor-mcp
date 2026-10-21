package billing

import (
	"bytes"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAcceptBook(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	var charged []string
	book(&charged, "cheque", "desk", "INV-9")
	if want := []string{"desk: INV-9"}; !reflect.DeepEqual(charged, want) {
		t.Errorf("book appended %q, want %q", charged, want)
	}
	if !strings.Contains(buf.String(), "cheque charge to desk") {
		t.Errorf("book logged %q", buf.String())
	}
	buf.Reset()
	w, v := &WalletPayer{}, &VoucherPayer{}
	Collect(Config{Payers: []Payer{&CardPayer{}, &BankPayer{}, w, v}}, []Account{{Owner: "Ann", Card: "4111222233334444", Bank: "DE89", Wallet: "0xab", Voucher: "SAVE"}}, Invoice{Ref: "Inv-2", Amount: 5})
	for _, s := range []string{"card charge to 4444", "bank charge to DE89", "wallet charge to 0xab", "voucher charge to SAVE"} {
		if !strings.Contains(buf.String(), s) {
			t.Errorf("charging logged %q, want %q in it", buf.String(), s)
		}
	}
	if !reflect.DeepEqual(w.Charged, []string{"0xab: Inv-2"}) || !reflect.DeepEqual(v.Charged, []string{"SAVE: inv-2"}) {
		t.Errorf("wallet charged %q, voucher charged %q", w.Charged, v.Charged)
	}
}
