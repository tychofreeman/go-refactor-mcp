package m

import "fmt"

func waterAs(b string, d bool) {
	fmt.Println("watering", b)
	if d {
		fmt.Println("deep", b)
	} else {
		fmt.Println("shallow", b)
	}
}
