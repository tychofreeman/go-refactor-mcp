package garden_test

import (
	"testing"

	"example.com/m/garden"
)

func TestWater(t *testing.T) {
	if got := garden.Water(garden.WaterArgs{Bed: "ferns", Litres: 2}); got != "ferns 2" {
		t.Fatal(got)
	}
}
