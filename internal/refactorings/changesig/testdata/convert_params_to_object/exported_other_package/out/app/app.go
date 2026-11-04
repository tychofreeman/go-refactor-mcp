package app

import "example.com/m/garden"

// Run waters the ferns.
func Run() string { return garden.Water(garden.WaterArgs{Bed: "ferns", Litres: 2}) }
