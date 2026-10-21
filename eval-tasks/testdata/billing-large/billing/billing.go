// Package billing charges accounts through several payment methods.
package billing

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// Account is someone to charge.
type Account struct {
	ID      string
	Owner   string
	Card    string // a card number
	Bank    string // a bank account number
	Wallet  string // a wallet address
	Voucher string // a voucher code
}

// Invoice is what is charged.
type Invoice struct {
	Ref    string
	Amount int // in cents
}

// Payer charges an invoice to an account through one method.
type Payer interface {
	Charge(a Account, inv Invoice)
	Name() string
}

// CardPayer charges a card.
type CardPayer struct {
	Charged []string // what was charged, "card: ref"
}

// Charge charges inv to a's card, which shows its last 4 digits.
func (c *CardPayer) Charge(a Account, inv Invoice) {
	log.Printf("card charge to %s", last(a.Card, 4))
	c.Charged = append(c.Charged, last(a.Card, 4)+": "+inv.Ref)
}

// Name is the method's name.
func (c *CardPayer) Name() string { return "card" }

// Receipt is what a card receipt for inv says.
func (c *CardPayer) Receipt(a Account, inv Invoice) string {
	return "card " + last(a.Card, 4) + " " + inv.Ref
}

// Masked is a card number as statements show it.
func (c *CardPayer) Masked(number string) string { return "****" + last(number, 4) }

// Tails is the last 4 digits of each card.
func (c *CardPayer) Tails(as []Account) []string {
	var out []string
	for _, a := range as {
		out = append(out, last(a.Card, 4))
	}
	return out
}

// Hint is a hint for a card on file, from its number.
func (c *CardPayer) Hint(n int) string { return last(fmt.Sprintf("%016d", n), 4) }

// BankPayer charges a bank account.
type BankPayer struct {
	Charged []string // what was charged, "bank: ref"
}

// Charge charges inv to a's bank account.
func (b *BankPayer) Charge(a Account, inv Invoice) {
	log.Printf("bank charge to %s", a.Bank)
	b.Charged = append(b.Charged, a.Bank+": "+inv.Ref+fmt.Sprintf(" %d", inv.Amount))
}

// Name is the method's name.
func (b *BankPayer) Name() string { return "bank" }

// WalletPayer charges a wallet.
type WalletPayer struct {
	Charged []string // what was charged, "wallet: ref"
}

// Charge charges inv to a's wallet.
func (w *WalletPayer) Charge(a Account, inv Invoice) {
	log.Printf("wallet charge to %s", a.Wallet)
	w.Charged = append(w.Charged, a.Wallet+": "+inv.Ref)
}

// Name is the method's name.
func (w *WalletPayer) Name() string { return "wallet" }

// VoucherPayer redeems a voucher.
type VoucherPayer struct {
	Charged []string // what was charged, "voucher: ref"
}

// Charge redeems a's voucher for inv.
func (v *VoucherPayer) Charge(a Account, inv Invoice) {
	log.Printf("voucher charge to %s", a.Voucher)
	v.Charged = append(v.Charged, a.Voucher+": "+strings.ToLower(inv.Ref))
}

// Name is the method's name.
func (v *VoucherPayer) Name() string { return "voucher" }

func last(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// Config says how Collect charges.
type Config struct {
	Payers []Payer
}

// Collect charges inv to each account through every payer, in the order
// given, and returns a summary line for each account: "owner: method,method".
func Collect(c Config, accounts []Account, inv Invoice) []string {
	var out []string
	for _, a := range accounts {
		var names []string
		for _, p := range c.Payers {
			p.Charge(a, inv)
			names = append(names, p.Name())
		}
		out = append(out, fmt.Sprintf("%s: %s", a.Owner, strings.Join(names, ",")))
	}
	return out
}

// SortAccounts sorts accounts in place by less.
func SortAccounts(accounts []Account, less func(a, b Account) bool) {
	sort.SliceStable(accounts, func(i, j int) bool { return less(accounts[i], accounts[j]) })
}
