package m

import "testing"

func TestCartTotal(t *testing.T) {}

func TestCartCheckout(t *testing.T) {
	cart := NewCart(0, "")
	checkout, err := cart.Checkout("")
	t.Fatalf("write the assertion: Checkout returned %v, %v", checkout, err)
}
