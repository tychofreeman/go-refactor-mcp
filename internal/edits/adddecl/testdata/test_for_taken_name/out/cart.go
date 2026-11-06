package m

import "errors"

// Cart holds items.
type Cart struct{ items []int }

// NewCart returns a cart with room for n items.
func NewCart(n int, owner string) *Cart { return &Cart{items: make([]int, 0, n)} }

// Total adds up the items above min.
func (c *Cart) Total(min int, tags []string) int { return 0 }

// Checkout pays for the cart.
func (c *Cart) Checkout(card string) (int, error) { return 0, errors.New("declined") }

// Discount is the share taken off a total.
func Discount(total int, member bool) float64 { return 0 }
