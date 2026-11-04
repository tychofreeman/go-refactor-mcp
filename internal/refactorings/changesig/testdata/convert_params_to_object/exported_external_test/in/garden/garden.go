package garden

import "fmt"

// Water gives bed litres of water.
func Water(bed string, litres int) string { return fmt.Sprint(bed, litres) }

func morning() string { return Water("roses", 3) }
