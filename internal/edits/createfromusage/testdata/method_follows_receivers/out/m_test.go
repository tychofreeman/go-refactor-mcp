package m

import "testing"

func TestTotal(t *testing.T) {
	p := &Plan{}
	p.Add(2)
	if got := p.Total(); got != 2 {
		t.Errorf("Total = %d", got)
	}
}
