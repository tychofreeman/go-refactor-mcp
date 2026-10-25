package gates

import "testing"

func TestAdmit(t *testing.T) {
	y := NewYard([]string{"d1"})
	y.Paid["t1"] = true
	if !y.Admit("t1") {
		t.Error("t1 not admitted")
	}
	if y.Taken["d1"] != "t1" {
		t.Errorf("taken = %v", y.Taken)
	}
}

func TestPassAndFits(t *testing.T) {
	y := NewYard(nil)
	if !y.Pass("t1", true) || y.Pass("t2", false) {
		t.Error("pass")
	}
	if !Fits(24) || Fits(0) {
		t.Error("fits")
	}
}

func TestOnTime(t *testing.T) {
	if !NewYard(nil, 7).OnTime() || NewYard(nil, 5).OnTime() {
		t.Error("on time")
	}
}
