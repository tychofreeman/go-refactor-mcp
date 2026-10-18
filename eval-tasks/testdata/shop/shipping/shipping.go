// Package shipping checks addresses and prints labels.
package shipping

import (
	"fmt"
	"strings"

	"example.com/shop/order"
)

// ValidAddress reports what is wrong with an address, if anything.
func ValidAddress(street, city, postcode string) error {
	var missing []string
	if strings.TrimSpace(street) == "" {
		missing = append(missing, "street")
	}
	if strings.TrimSpace(city) == "" {
		missing = append(missing, "city")
	}
	if strings.TrimSpace(postcode) == "" {
		missing = append(missing, "postcode")
	}
	if len(missing) > 0 {
		return fmt.Errorf("address is missing %s", strings.Join(missing, ", "))
	}
	if len(postcode) != 5 || strings.Trim(postcode, "0123456789") != "" {
		return fmt.Errorf("postcode %q must have 5 digits", postcode)
	}
	return nil
}

// Label is the shipping label for o.
func Label(o order.Order) string {
	return fmt.Sprintf("%s\n%s\n%s %s\n", o.Customer, o.Street, o.Postcode, o.City)
}

// Ship checks o's address and returns its label.
func Ship(o order.Order) (string, error) {
	if err := ValidAddress(o.Street, o.City, o.Postcode); err != nil {
		return "", fmt.Errorf("order %s: %w", o.ID, err)
	}
	return Label(o), nil
}
