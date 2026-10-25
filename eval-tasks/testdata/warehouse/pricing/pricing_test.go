package pricing

import "testing"

func TestPrice(t *testing.T) {
	if got := Price(100); got != 125 {
		t.Errorf("Price(100) = %v", got)
	}
	if got := Quote(100, 3); got != 128 {
		t.Errorf("Quote = %v", got)
	}
	if got := NewRate("acme", 2, 5).Storage(10); got != 30 {
		t.Errorf("Storage = %v", got)
	}
	if got := Floor(2); got != 1 {
		t.Errorf("Floor = %v", got)
	}
}
