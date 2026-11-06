package m

import "testing"

func TestCartTotal(t *testing.T) {
	cart := NewCart(0, "")
	total := cart.Total(0, nil)
	t.Fatalf("write the assertion: Total returned %v", total)
}
