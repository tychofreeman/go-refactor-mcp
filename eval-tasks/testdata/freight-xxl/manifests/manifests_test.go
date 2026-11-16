package manifests

import (
	"reflect"
	"testing"

	"example.com/freight/freight"
)

var ss = []freight.Shipment{
	{ID: "2", Sender: "Bo", Road: "nw-0042", Rail: "Y7"},
	{ID: "1", Sender: "Ann", Road: "se-1111"},
	{ID: "3", Sender: "Cy", Rail: "Y9"},
}

func TestManifests(t *testing.T) {
	if got, want := Register(ss[:1]), []string{"Bo [nw-0042] Y7"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Register = %q, want %q", got, want)
	}
	if got, want := MissingRoad(ss), []string{"Cy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingRoad = %q", got)
	}
	if got, want := MissingRail(ss), []string{"Ann"}; !reflect.DeepEqual(got, want) {
		t.Errorf("MissingRail = %q", got)
	}
	if got := RoadRegion(ss[0]); got != "NW" {
		t.Errorf("RoadRegion = %q", got)
	}
	if got := CSV(ss); got != "1,Ann,se-1111,\n2,Bo,nw-0042,Y7\n3,Cy,,Y9\n" {
		t.Errorf("CSV = %q", got)
	}
	if got := Label(ss[2]); got != "Cy (Y9)" {
		t.Errorf("Label = %q", got)
	}
	if got := RailCounts(ss); got["Y7"] != 1 || got["Y9"] != 1 || len(got) != 2 {
		t.Errorf("RailCounts = %v", got)
	}
	if got := RoadLine(ss[2]); got != "Cy: no road" {
		t.Errorf("RoadLine = %q", got)
	}
}
