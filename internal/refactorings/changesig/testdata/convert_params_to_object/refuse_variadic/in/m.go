package m

import "fmt"

// water waters beds.
func water(litres int, beds ...string) { fmt.Println(litres, beds) }

func morning() { water(2, "roses", "herbs") }
