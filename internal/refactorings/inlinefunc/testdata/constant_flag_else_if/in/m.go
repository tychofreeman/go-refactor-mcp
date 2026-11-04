package m

import "fmt"

// sow sows bed thick, thin or not at all.
func sow(bed string, thick, thin bool) {
	if thick {
		fmt.Println("thick", bed)
	} else if thin {
		fmt.Println("thin", bed)
	}
}

func sowThin(b string) { sow(b, false, true) }
