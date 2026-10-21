package billing

import "testing"

func TestAcceptTail(t *testing.T) {
	c := &CardPayer{}
	if got := c.tail("12"); got != "12" {
		t.Errorf("tail(12) = %q", got)
	}
	if got := c.tail("4111222233334444"); got != "4444" {
		t.Errorf("tail cut to %q, want the last 4 bytes", got)
	}
	if got := c.Receipt(Account{Card: "5500000000000004"}, Invoice{Ref: "R"}); got != "card 0004 R" {
		t.Errorf("Receipt = %q", got)
	}
	if got := c.Tails([]Account{{Card: "1"}, {Card: "4111222233334444"}}); len(got) != 2 || got[1] != "4444" {
		t.Errorf("Tails = %q", got)
	}
}
