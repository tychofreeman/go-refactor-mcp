package lending

import "testing"

func TestAcceptRenewals(t *testing.T) {
	for kind, want := range map[string]int{"book": 3, "dvd": 1, "map": 2, "atlas": 0, "magazine": 0} {
		it := Item{Title: "T", Kind: kind}
		if got := MaxRenewals(it); got != want {
			t.Errorf("MaxRenewals(%s) = %d, want %d", kind, got, want)
		}
		if Renewable(it) != (want > 0) {
			t.Errorf("Renewable(%s) = %v", kind, Renewable(it))
		}
	}
}
