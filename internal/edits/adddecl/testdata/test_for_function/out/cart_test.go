package m

import "testing"

func TestCartTotal(t *testing.T) {}

func TestDiscount(t *testing.T) {
	discount := Discount(0, false)
	t.Fatalf("write the assertion: Discount returned %v", discount)
}
