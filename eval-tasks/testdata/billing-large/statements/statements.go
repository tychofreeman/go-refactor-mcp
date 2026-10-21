// Package statements lists accounts' payment details.
package statements

import (
	"fmt"
	"sort"
	"strings"

	"example.com/billing/billing"
)

// Register is a line per account: owner, card and bank.
func Register(as []billing.Account) []string {
	var out []string
	for _, a := range as {
		out = append(out, fmt.Sprintf("%s [%s] %s", a.Owner, a.Card, a.Bank))
	}
	return out
}

// MissingCard lists accounts without a card.
func MissingCard(as []billing.Account) []string {
	var out []string
	for _, a := range as {
		if a.Card == "" {
			out = append(out, a.Owner)
		}
	}
	return out
}

// MissingBank lists accounts without a bank account.
func MissingBank(as []billing.Account) []string {
	var out []string
	for _, a := range as {
		if a.Bank == "" {
			out = append(out, a.Owner)
		}
	}
	return out
}

// CardIssuer is the issuer of a's card, by its first digit.
func CardIssuer(a billing.Account) string {
	if a.Card == "" {
		return ""
	}
	switch a.Card[0] {
	case '4':
		return "visa"
	case '5':
		return "mastercard"
	}
	return "other"
}

// SameCard reports whether a and b pay with the same card.
func SameCard(a, b billing.Account) bool {
	return strings.TrimSpace(a.Card) == strings.TrimSpace(b.Card)
}

// BankCountry is the country code that starts a's bank account number.
func BankCountry(a billing.Account) string {
	if len(a.Bank) < 2 {
		return ""
	}
	return strings.ToUpper(a.Bank[:2])
}

// ByCard indexes accounts by card number.
func ByCard(as []billing.Account) map[string]billing.Account {
	m := map[string]billing.Account{}
	for _, a := range as {
		m[a.Card] = a
	}
	return m
}

// CSV lists accounts as "id,owner,card,bank" lines, by ID.
func CSV(as []billing.Account) string {
	sorted := append([]billing.Account(nil), as...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var b strings.Builder
	for _, a := range sorted {
		fmt.Fprintf(&b, "%s,%s,%s,%s\n", a.ID, a.Owner, a.Card, a.Bank)
	}
	return b.String()
}

// Payable reports whether a can be charged by card or bank.
func Payable(a billing.Account) bool { return a.Card != "" || a.Bank != "" }

// Label is how an account is shown: owner and the first method there is.
func Label(a billing.Account) string {
	switch {
	case a.Card != "":
		return a.Owner + " (" + a.Card + ")"
	case a.Bank != "":
		return a.Owner + " (" + a.Bank + ")"
	}
	return a.Owner
}

// Issuers counts accounts by card issuer.
func Issuers(as []billing.Account) map[string]int {
	m := map[string]int{}
	for _, a := range as {
		m[CardIssuer(a)]++
	}
	return m
}
