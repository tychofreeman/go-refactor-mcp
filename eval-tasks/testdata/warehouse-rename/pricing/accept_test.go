package pricing

import "testing"

func TestAcceptRenamed(t *testing.T) {
	if markup != 0.25 {
		t.Errorf("markup = %v", markup)
	}
}

func TestAcceptPrices(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"Bulk", Bulk(10, 2, 3), 28},
		{"Quote", Quote(100, 3), 128},
		{"Reduced", Reduced(100, 0.05), 120},
		{"Reduced below zero", Reduced(100, 0.5), 100},
		{"Storage", NewRate("acme", 2, 5).Storage(10), 30},
		{"Floor", Floor(40), 10},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}
