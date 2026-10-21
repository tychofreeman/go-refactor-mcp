// Package runs makes the regular billing runs.
package runs

import "example.com/billing/billing"

// Rent charges the rent invoice to accounts.
func Rent(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "RENT-1", Amount: 1000})
}

// Utilities charges the utilities invoice to accounts.
func Utilities(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "UTIL-2", Amount: 2000})
}

// Membership charges the membership invoice to accounts.
func Membership(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "MEMB-3", Amount: 3000})
}

// Insurance charges the insurance invoice to accounts.
func Insurance(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "INSU-4", Amount: 4000})
}

// Parking charges the parking invoice to accounts.
func Parking(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "PARK-5", Amount: 5000})
}

// Storage charges the storage invoice to accounts.
func Storage(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "STOR-6", Amount: 6000})
}

// Licence charges the licence invoice to accounts.
func Licence(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "LICE-7", Amount: 7000})
}

// Support charges the support invoice to accounts.
func Support(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "SUPP-8", Amount: 8000})
}

// Hosting charges the hosting invoice to accounts.
func Hosting(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "HOST-9", Amount: 9000})
}

// Training charges the training invoice to accounts.
func Training(c billing.Config, accounts []billing.Account) []string {
	return billing.Collect(c, accounts, billing.Invoice{Ref: "TRAI-10", Amount: 10000})
}
