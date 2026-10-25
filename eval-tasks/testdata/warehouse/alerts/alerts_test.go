package alerts

import (
	"reflect"
	"testing"
)

func TestProbe(t *testing.T) {
	var alerts []string
	Temp{"cold", 5}.Probe(7, &alerts)
	Flood{"basement", 1}.Probe(0.5, &alerts)
	Gas{"dock", 30}.Probe(31, &alerts)
	want := []string{"cold: temp 7.0 over 5.0", "dock: gas 31.0 over 30.0"}
	if !reflect.DeepEqual(alerts, want) {
		t.Errorf("alerts = %q, want %q", alerts, want)
	}
}
