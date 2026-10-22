// Package desk settles the loans returned at each of the library's desks.
package desk

import "example.com/lending/lending"

// Main settles the loans returned at the main desk on day.
func Main(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+1)
}

// North settles the loans returned at the north desk on day.
func North(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+2)
}

// South settles the loans returned at the south desk on day.
func South(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+0)
}

// East settles the loans returned at the east desk on day.
func East(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+1)
}

// West settles the loans returned at the west desk on day.
func West(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+2)
}

// Harbor settles the loans returned at the harbor desk on day.
func Harbor(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+0)
}

// Campus settles the loans returned at the campus desk on day.
func Campus(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+1)
}

// Hospital settles the loans returned at the hospital desk on day.
func Hospital(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+2)
}

// Prison settles the loans returned at the prison desk on day.
func Prison(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+0)
}

// School settles the loans returned at the school desk on day.
func School(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+1)
}

// Market settles the loans returned at the market desk on day.
func Market(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+2)
}

// Station settles the loans returned at the station desk on day.
func Station(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+0)
}

// Airport settles the loans returned at the airport desk on day.
func Airport(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+1)
}

// Museum settles the loans returned at the museum desk on day.
func Museum(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+2)
}

// Mobile settles the loans returned at the mobile desk on day.
func Mobile(loans []lending.Loan, day int) []lending.Charge {
	return lending.Settle(loans, day+0)
}

// Rare settles the loans of atlases returned at any desk on day.
func Rare(loans []lending.Loan, day int) []lending.Charge {
	var rare []lending.Loan
	for _, l := range loans {
		if l.Item.Kind == "atlas" {
			rare = append(rare, l)
		}
	}
	return lending.Settle(rare, day)
}

// Owed is the total each member owes for loans returned on day.
func Owed(loans []lending.Loan, day int) map[string]int {
	owed := map[string]int{}
	for _, c := range lending.Settle(loans, day) {
		owed[c.Member] += c.Fee
	}
	return owed
}
