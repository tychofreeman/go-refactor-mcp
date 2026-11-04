package m

import "fmt"

// water waters bed, deeply or not.
func water(bed string, deep bool) {
	fmt.Println("watering", bed)
	if deep {
		fmt.Println("deep", bed)
	} else {
		fmt.Println("shallow", bed)
	}
}

func waterShallow(b string) { water(b, false) }
