package m

import "fmt"

// mist mists bed unless dry.
func mist(bed string, dry bool) {
	if !dry {
		fmt.Println("mist", bed)
	}
	fmt.Println("done", bed)
}

func mistDry(b string) { mist(b, true) }
