package garden

import "fmt"

// WaterArgs holds the arguments of Water.
type WaterArgs struct {
	Bed    string
	Litres int
}

// Water gives bed litres of water.
func Water(args WaterArgs) string {
	return fmt.Sprint(args.Bed, args.Litres)
}

func morning() string { return Water(WaterArgs{Bed: "roses", Litres: 3}) }
